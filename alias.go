package main

// Unaliased-slice analysis (#578, step 1).
//
// Answers one question per local slice variable: does any OTHER live reference
// observe this slice's backing array? If not, `append` may grow it in place
// instead of allocating a fresh block and memcpy'ing — the copy-and-abandon path
// that costs machin the sieve benchmark (79ms of appends against Rust's 27ms; a
// prototype that grew in place hit parity).
//
// SHIPS NO CODEGEN CHANGE. This is the proof, exposed for review; the
// optimization is a later step gated on it.
//
// FAIL-CLOSED BY CONSTRUCTION. Every operation on a candidate must be on an
// explicit whitelist of forms that provably cannot leak a reference to the
// backing array. Anything else — including an AST node this analysis does not
// recognise — disqualifies the variable. The cost of a case nobody thought of is
// therefore a missed optimization, never a dangling pointer.
//
// The rejected alternative was a dynamic `shared` bit set at each header-copy
// site. It is less code, but it fails OPEN: a copy site nobody thought of leaves
// an alias unmarked, and an unmarked alias is silent memory corruption.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// SliceAlias is one local slice variable's verdict.
type SliceAlias struct {
	Fn        string `json:"fn"`               // instantiation name
	Var       string `json:"var"`              // local variable
	Unaliased bool   `json:"unaliased"`        // safe to grow in place
	Reason    string `json:"reason,omitempty"` // why it was disqualified
	Appends   int    `json:"appends"`          // `v = append(v, …)` sites; 0 means nothing to win
}

// aliasScan records WHERE each event happens, not just whether it happens.
//
// Flow sensitivity is what makes this analysis useful rather than merely sound. A
// flow-INsensitive version disqualified almost every real slice: the dominant
// shape is "build it, then use it" — append in a loop, then pass the finished
// slice to json()/a helper/a return — and the aliasing use comes AFTER the last
// append, where it cannot observe a moved array.
//
// Top-level statements execute in order, so an aliasing use at a strictly greater
// top-level index provably runs after every append. Events inside a nested block
// carry that block's top-level index, so an alias and an append in the SAME loop
// collide (equal indices) and correctly disqualify.
type aliasScan struct {
	cand   map[string]bool   // candidate -> known at all
	topIdx int               // top-level statement index currently being scanned
	appAt  map[string]int    // candidate -> highest top-level index of an append
	aliasA map[string]int    // candidate -> lowest top-level index of an aliasing use
	why    map[string]string // candidate -> reason for that lowest aliasing use
	apps   map[string]int    // candidate -> self-append count
}

func (s *aliasScan) isCand(name string) bool { return s.cand[name] }

// disq records an ALIASING USE at the current top-level index. Whether it
// actually disqualifies is decided later, against where the appends are.
func (s *aliasScan) disq(name, reason string) {
	if !s.cand[name] {
		return
	}
	if prev, seen := s.aliasA[name]; !seen || s.topIdx < prev {
		s.aliasA[name] = s.topIdx
		s.why[name] = reason
	}
}

// noteAppend records a self-append at the current top-level index.
func (s *aliasScan) noteAppend(name string) {
	s.apps[name]++
	if prev, seen := s.appAt[name]; !seen || s.topIdx > prev {
		s.appAt[name] = s.topIdx
	}
}

// scanExpr walks an expression allowing ONLY the read forms that cannot leak a
// reference to a candidate's backing array. A bare mention of a candidate copies
// its header, which is exactly what creates an alias, so it disqualifies.
func (s *aliasScan) scanExpr(e Expr) {
	switch t := e.(type) {
	case nil:
		return
	case *IntLit, *FloatLit, *StringLit, *BoolLit, *NilLit, *MakeChan, *MakeMap:
		return
	case *Ident:
		s.disq(t.Name, "used as a bare value, which copies the slice header and aliases the array")
	case *Index:
		// v[i] yields an ELEMENT. For a struct or nested-slice element that is a
		// copy of the element, not a pointer into the outer array, so the outer
		// array may still move.
		if id, ok := t.X.(*Ident); ok && s.isCand(id.Name) {
			// allowed
		} else {
			s.scanExpr(t.X)
		}
		s.scanExpr(t.Idx)
	case *Call:
		if t.Callee == "len" && len(t.Args) == 1 {
			if id, ok := t.Args[0].(*Ident); ok && s.isCand(id.Name) {
				return // len(v) reads the header field only
			}
		}
		for _, a := range t.Args {
			s.scanExpr(a)
		}
	case *CallValue:
		s.scanExpr(t.Fn)
		for _, a := range t.Args {
			s.scanExpr(a)
		}
	case *Binary:
		s.scanExpr(t.L)
		s.scanExpr(t.R)
	case *Unary:
		s.scanExpr(t.X)
	case *FieldAccess:
		s.scanExpr(t.X)
	case *SliceExpr:
		// s[lo:hi] copies, so the RESULT is not an alias — but taking a range of a
		// candidate is rare enough that allowing it is not worth the argument.
		s.scanExpr(t.X)
		s.scanExpr(t.Lo)
		s.scanExpr(t.Hi)
	case *SliceLit:
		for _, el := range t.Elems {
			s.scanExpr(el)
		}
	case *StructLit:
		for _, v := range t.Vals {
			s.scanExpr(v)
		}
	case *Recv:
		s.scanExpr(t.Ch)
	case *MakeClosure:
		// A captured variable is shared by reference with the closure, which may
		// outlive this frame.
		for _, c := range t.Captures {
			s.disq(c, "captured by a closure, which shares it by reference")
		}
	case *FuncLit:
		// Pre-lifting shape; treat its whole body as opaque.
		s.scanStmts(t.Body)
	default:
		// An unrecognised expression node. Fail closed: disqualify everything, since
		// we cannot argue about what it does with the value.
		for name := range s.cand {
			s.disq(name, "appears in an expression form this analysis does not model")
		}
	}
}

func (s *aliasScan) scanStmts(body []Stmt) {
	for _, st := range body {
		s.scanStmt(st)
	}
}

func (s *aliasScan) scanStmt(st Stmt) {
	switch t := st.(type) {
	case nil:
		return
	case *AssignStmt:
		// The one shape that keeps a candidate alive: v = append(v, …).
		if call, ok := t.Val.(*Call); ok && call.Callee == "append" && len(call.Args) >= 1 {
			if id, ok := call.Args[0].(*Ident); ok && id.Name == t.Name && s.isCand(t.Name) {
				if call.Spread {
					s.disq(t.Name, "grown with a spread append, which this analysis does not model")
				} else {
					s.noteAppend(t.Name)
				}
				for _, a := range call.Args[1:] {
					s.scanExpr(a) // appending a candidate stores ITS header into the array
				}
				return
			}
		}
		// Initialization from a literal keeps it a candidate.
		if lit, ok := t.Val.(*SliceLit); ok && s.isCand(t.Name) {
			for _, el := range lit.Elems {
				s.scanExpr(el)
			}
			return
		}
		// Any other assignment INTO a candidate makes it share whatever it was given.
		if s.isCand(t.Name) {
			s.disq(t.Name, "assigned a value other than append(v, …) or a slice literal")
		}
		s.scanExpr(t.Val)
	case *MultiAssign:
		for _, n := range t.Names {
			s.disq(n, "assigned as part of a multi-assignment")
		}
		for _, r := range t.Rhs {
			s.scanExpr(r)
		}
	case *IndexAssign:
		if id, ok := t.Target.X.(*Ident); ok && s.isCand(id.Name) {
			// v[i] = e writes THROUGH the array; no reference escapes.
		} else {
			s.scanExpr(t.Target.X)
		}
		s.scanExpr(t.Target.Idx)
		s.scanExpr(t.Val)
	case *FieldAssign:
		// v[i].f = e — the base is an Index over the candidate.
		if idx, ok := t.Target.X.(*Index); ok {
			if id, ok := idx.X.(*Ident); ok && s.isCand(id.Name) {
				s.scanExpr(idx.Idx)
				s.scanExpr(t.Val)
				return
			}
		}
		s.scanExpr(t.Target.X)
		s.scanExpr(t.Val)
	case *ExprStmt:
		s.scanExpr(t.X)
	case *ReturnStmt:
		for _, v := range t.Vals {
			s.scanExpr(v) // returning a candidate hands its array to the caller
		}
	case *IfStmt:
		s.scanExpr(t.Cond)
		s.scanStmts(t.Then)
		s.scanStmts(t.Else)
	case *WhileStmt:
		s.scanExpr(t.Cond)
		s.scanStmts(t.Body)
	case *RangeStmt:
		if id, ok := t.X.(*Ident); ok && s.isCand(id.Name) {
			// for _, v := range xs iterates by value.
		} else {
			s.scanExpr(t.X)
		}
		s.scanStmts(t.Body)
	case *SendStmt:
		// The runtime deep-copies values crossing a channel, so a send arguably does
		// not alias. Disqualified anyway for v1: the optimization is not worth
		// arguing that subtlety before there is a reason to.
		s.scanExpr(t.Ch)
		if id, ok := t.Val.(*Ident); ok {
			s.disq(id.Name, "sent on a channel (conservative: the copy is not modelled here yet)")
		} else {
			s.scanExpr(t.Val)
		}
	case *GoStmt:
		for _, a := range t.Call.Args {
			s.scanExpr(a)
		}
	case *ArenaStmt:
		s.scanStmts(t.Body)
	case *SelectStmt:
		for _, cse := range t.Cases {
			s.scanExpr(cse.RecvCh)
			s.scanExpr(cse.SendCh)
			s.scanExpr(cse.SendVal)
			s.scanStmts(cse.Body)
		}
		s.scanStmts(t.Default)
	case *BreakStmt, *ContinueStmt:
		return
	default:
		// Unrecognised statement: fail closed.
		for name := range s.cand {
			s.disq(name, "appears in a statement form this analysis does not model")
		}
	}
}

// analyzeSliceAliasing returns a verdict for every local slice variable in every
// instantiated function, sorted for stable output.
func analyzeSliceAliasing(prog *Program, c *Checker) []SliceAlias {
	var out []SliceAlias
	for _, inst := range c.Reps() {
		fn := c.SrcFunc(inst)
		if fn == nil {
			continue
		}
		s := &aliasScan{
			cand: map[string]bool{}, why: map[string]string{},
			apps: map[string]int{}, appAt: map[string]int{}, aliasA: map[string]int{},
		}
		// Parameters are candidates too — not because they can ever qualify, but so
		// a function that appends to one is REPORTED and refused rather than
		// silently absent from the review.
		for _, name := range append(append([]string{}, fn.Params...), c.Locals(inst)...) {
			if c.VarKind(inst, name) == KSlice {
				s.cand[name] = true
			}
		}
		if len(s.cand) == 0 {
			continue
		}
		// A parameter is a slice the CALLER already holds, so the alias exists
		// before the body runs — index -1, ahead of every append.
		s.topIdx = -1
		for _, p := range fn.Params {
			if s.cand[p] {
				s.disq(p, "is a parameter, so the caller holds the same array before any append")
			}
		}
		// Scan the body top level, tracking which statement we are inside.
		for i, st := range fn.Body {
			s.topIdx = i
			s.scanStmt(st)
		}

		names := make([]string, 0, len(s.cand))
		for n := range s.cand {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			aliasIdx, aliased := s.aliasA[n]
			appIdx, appended := s.appAt[n]
			// An aliasing use only matters if it can observe an array that a LATER
			// append moves. Equal indices mean both are inside the same top-level
			// statement (a loop or branch), where order cannot be established.
			unsafe := aliased && (!appended || aliasIdx <= appIdx)
			reason := ""
			if unsafe {
				reason = s.why[n]
			}
			out = append(out, SliceAlias{
				Fn: inst, Var: n,
				Unaliased: !unsafe,
				Reason:    reason,
				Appends:   s.apps[n],
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Fn != out[j].Fn {
			return out[i].Fn < out[j].Fn
		}
		return out[i].Var < out[j].Var
	})
	return out
}

// AliasReport is `machin alias`'s whole verdict.
type AliasReport struct {
	OK        bool         `json:"ok"`
	Files     []string     `json:"files"`
	Unaliased int          `json:"unaliased"` // qualifying slices that are actually appended to
	Total     int          `json:"total"`
	Slices    []SliceAlias `json:"slices"`
}

// cmdAlias reports which local slices are provably unaliased, and why the rest
// are not (#578 step 1). It is a REPORT: no codegen consumes this yet. The point
// is that the proof is reviewable before anything depends on it.
func cmdAlias(args []string) error {
	jsonOut := false
	var files []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else {
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("alias: need at least one source file")
	}
	prog, _, err := composeSources(files)
	if err != nil {
		return err
	}
	liftClosures(prog)
	c, err := Check(prog)
	if err != nil {
		return err
	}
	slices := analyzeSliceAliasing(prog, c)

	rep := AliasReport{OK: true, Files: files, Slices: slices, Total: len(slices)}
	for _, s := range slices {
		if s.Unaliased && s.Appends > 0 {
			rep.Unaliased++
		}
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	for _, s := range slices {
		mark := "no "
		if s.Unaliased {
			mark = "YES"
		}
		fmt.Printf("  %-3s %s.%s", mark, s.Fn, s.Var)
		if s.Appends > 0 {
			fmt.Printf("  (%d append site(s))", s.Appends)
		}
		if !s.Unaliased {
			fmt.Printf("  — %s", s.Reason)
		}
		fmt.Println()
	}
	fmt.Printf("\n%d of %d local slices are provably unaliased AND appended to\n", rep.Unaliased, rep.Total)
	fmt.Println("(a report only — no codegen consumes this yet; see issue #578)")
	return nil
}
