package main

// Slice 1.1 — inferred data-race detection (type-aware, reads + writes).
//
// The "better than Rust" concurrency guarantee, with ZERO Send/Sync annotations:
// infer which heap locations are shared *and* concurrently accessed across goroutine
// boundaries, at least one access a write, and report the race with a counterexample.
//
// Two-stage design:
//   1. A structural, per-function ACCESS SUMMARY (fixed-point over the call graph):
//      for each function, which access PATHS (index / field chains) through each
//      parameter are read or written — directly or transitively via callees.
//   2. A per-instance RACE PASS: at each `go f(args)` the goroutine's param accesses
//      are re-rooted onto the caller's argument; combined with the spawner's own
//      accesses, a root reached by >=2 concurrent accessors (>=1 write) is a race —
//      but only if the access TYPE actually reaches shared heap (a slice/map indirection).
//
// Sharing is reachability-based (empirically grounded): structs pass by value, so a
// scalar-field write on a struct arg races nothing; but a slice *inside* that struct
// keeps its shared backing array, so `s.items[i] = v` does race. `typeShared` walks the
// access path against the argument's resolved type to decide.
//
// Soundness stance (a borrow checker's): concurrency is over-approximated (every
// goroutine in a scope is treated as live alongside the spawner) and slice indices are
// ignored — sound, may over-report, never under-reports. Happens-before precision
// (channel joins) lands in Slice 1.4.

import (
	"fmt"
	"sort"
	"strings"
)

// ── access paths ────────────────────────────────────────────────────────────
// An accStep is one navigation from a root: an index step ([], field == "") or a
// struct-field step (field name). A path is the chain from the root outward.
type accStep struct{ field string }
type accPath []accStep

func clonePath(p accPath) accPath {
	if len(p) == 0 {
		return nil
	}
	q := make(accPath, len(p))
	copy(q, p)
	return q
}

func pathEq(a, b accPath) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// placeOf decomposes a place expression into (rootIdent, path). Returns ok=false
// when the expression is not rooted in a named variable.
func placeOf(e Expr) (string, accPath, bool) {
	switch t := e.(type) {
	case *Ident:
		return t.Name, nil, true
	case *Index:
		r, p, ok := placeOf(t.X)
		if !ok {
			return "", nil, false
		}
		return r, append(clonePath(p), accStep{}), true
	case *FieldAccess:
		r, p, ok := placeOf(t.X)
		if !ok {
			return "", nil, false
		}
		return r, append(clonePath(p), accStep{field: t.Name}), true
	}
	return "", nil, false
}

// ── stage 1: the per-function access summary ────────────────────────────────
type paramAcc struct {
	idx   int
	path  accPath
	write bool
}

// accessSummary computes, for every function, the parameter accesses it performs —
// directly and transitively through calls / goroutine spawns — to a fixed point.
func accessSummary(prog *Program) map[string][]paramAcc {
	sum := map[string][]paramAcc{}
	for _, f := range prog.Funcs {
		sum[f.Name] = nil
	}
	add := func(fn string, a paramAcc) bool {
		for _, e := range sum[fn] {
			if e.idx == a.idx && e.write == a.write && pathEq(e.path, a.path) {
				return false
			}
		}
		sum[fn] = append(sum[fn], paramAcc{a.idx, clonePath(a.path), a.write})
		return true
	}

	for changed := true; changed; {
		changed = false
		for _, f := range prog.Funcs {
			pidx := map[string]int{}
			for i, p := range f.Params {
				pidx[p] = i
			}
			record := func(root string, path accPath, write bool) {
				if i, ok := pidx[root]; ok {
					if add(f.Name, paramAcc{i, path, write}) {
						changed = true
					}
				}
			}
			// a call/go: re-root each callee param access onto our matching arg.
			propCall := func(callee string, args []Expr) {
				for j, a := range args {
					r, pfx, ok := placeOf(a)
					if !ok {
						continue
					}
					pi, isParam := pidx[r]
					if !isParam {
						continue
					}
					for _, ca := range sum[callee] {
						if ca.idx != j {
							continue
						}
						full := append(clonePath(pfx), ca.path...)
						if add(f.Name, paramAcc{pi, full, ca.write}) {
							changed = true
						}
					}
				}
			}
			var visitExpr func(e Expr)
			visitExpr = func(e Expr) {
				switch t := e.(type) {
				case nil:
					return
				case *Index:
					if r, p, ok := placeOf(t); ok {
						record(r, p, false) // element read
					}
					visitExpr(t.X)
					visitExpr(t.Idx)
				case *FieldAccess:
					if r, p, ok := placeOf(t); ok {
						record(r, p, false)
					}
					visitExpr(t.X)
				case *Call:
					propCall(t.Callee, t.Args)
					for _, a := range t.Args {
						visitExpr(a)
					}
				case *Binary:
					visitExpr(t.L)
					visitExpr(t.R)
				case *Unary:
					visitExpr(t.X)
				case *SliceLit:
					for _, el := range t.Elems {
						visitExpr(el)
					}
				case *StructLit:
					for _, v := range t.Vals {
						visitExpr(v)
					}
				}
			}
			var visit func(ss []Stmt)
			visit = func(ss []Stmt) {
				for _, s := range ss {
					switch st := s.(type) {
					case *IndexAssign:
						if r, p, ok := placeOf(st.Target); ok {
							record(r, p, true)
						}
						visitExpr(st.Target.Idx)
						visitExpr(st.Val)
					case *FieldAssign:
						if r, p, ok := placeOf(st.Target); ok {
							record(r, p, true)
						}
						visitExpr(st.Val)
					case *AssignStmt:
						visitExpr(st.Val)
					case *MultiAssign:
						for _, e := range st.Rhs {
							visitExpr(e)
						}
					case *ExprStmt:
						visitExpr(st.X)
					case *ReturnStmt:
						for _, e := range st.Vals {
							visitExpr(e)
						}
					case *SendStmt:
						visitExpr(st.Ch)
						visitExpr(st.Val)
					case *GoStmt:
						propCall(st.Call.Callee, st.Call.Args)
						for _, a := range st.Call.Args {
							visitExpr(a)
						}
					case *IfStmt:
						visitExpr(st.Cond)
						visit(st.Then)
						visit(st.Else)
					case *WhileStmt:
						visitExpr(st.Cond)
						visit(st.Body)
					case *RangeStmt:
						visitExpr(st.X)
						visit(st.Body)
					case *ArenaStmt:
						visit(st.Body)
					}
				}
			}
			visit(f.Body)
		}
	}
	return sum
}

// ── stage 2: the race pass ──────────────────────────────────────────────────
type raceFinding struct {
	Root    string   // the shared variable
	Decl    string   // source function the concurrent accesses live in
	Kind    string   // "write/write" | "read/write"
	Writers []string // counterexample: each concurrent accessor
}

type accessor struct {
	write bool
	byGo  bool
	desc  string
}

// slotTypeName renders a checker type slot as an MFL type string ("[]int",
// "map[string]int", "Box", ...) so reachability can walk it uniformly with the
// field-type strings the struct table returns.
func slotTypeName(c *Checker, slot int) string {
	s := c.find(slot)
	switch c.kind[s] {
	case KInt, KNum:
		return "int"
	case KFloat:
		return "float"
	case KBool:
		return "bool"
	case KString:
		return "string"
	case KBytes:
		return "bytes"
	case KSlice:
		if c.elem[s] >= 0 {
			return "[]" + slotTypeName(c, c.elem[s])
		}
		return "[]?"
	case KMap:
		if c.mkey[s] >= 0 && c.mval[s] >= 0 {
			return "map[" + slotTypeName(c, c.mkey[s]) + "]" + slotTypeName(c, c.mval[s])
		}
		return "map[?]?"
	case KChan:
		if c.elem[s] >= 0 {
			return "chan " + slotTypeName(c, c.elem[s])
		}
		return "chan ?"
	case KStruct:
		return c.sname[s]
	}
	return "?"
}

// typeShared reports whether an access path, applied to a value of type tname,
// reaches SHARED heap — i.e. it dereferences through a slice/map (whose backing
// store is aliased across the goroutine boundary) at or before the accessed
// location. Struct navigation on a by-value copy stays private until it crosses
// such an indirection.
func typeShared(c *Checker, tname string, path accPath) bool {
	cur := tname
	shared := false
	for _, step := range path {
		if step.field == "" { // index step
			switch {
			case strings.HasPrefix(cur, "[]"):
				shared = true
				cur = cur[2:]
			case strings.HasPrefix(cur, "map["):
				shared = true
				cur = mapValType(cur)
			default:
				return shared // indexing a string/bytes: no struct sharing beyond here
			}
		} else { // field step
			ft, ok := c.fieldType(cur, step.field)
			if !ok {
				return shared
			}
			cur = ft
		}
	}
	return shared
}

// mapValType extracts V from "map[K]V" (K has no unbalanced brackets in practice).
func mapValType(t string) string {
	depth := 0
	for i := 4; i < len(t); i++ {
		switch t[i] {
		case '[':
			depth++
		case ']':
			if depth == 0 {
				return t[i+1:]
			}
			depth--
		}
	}
	return "?"
}

// detectRaces runs the inferred data-race analysis over a type-checked program.
func detectRaces(prog *Program, c *Checker) []raceFinding {
	sum := accessSummary(prog)
	var out []raceFinding
	seen := map[string]bool{}

	for _, inst := range c.reps {
		f := c.instFn[inst]
		if f == nil {
			continue
		}
		// gather accesses to each root within this instance's body
		accs := map[string][]accessor{}
		note := func(root string, path accPath, write, byGo bool, desc string) {
			slot, ok := c.vars[inst][root]
			if !ok {
				return // not a local/param of this instance (globals: Slice 1.2)
			}
			if !typeShared(c, slotTypeName(c, slot), path) {
				return // access doesn't reach shared heap -> cannot race
			}
			accs[root] = append(accs[root], accessor{write, byGo, desc})
		}
		var visit func(ss []Stmt)
		visit = func(ss []Stmt) {
			for _, s := range ss {
				switch st := s.(type) {
				case *GoStmt:
					g := st.Call.Callee
					for j, a := range st.Call.Args {
						r, pfx, ok := placeOf(a)
						if !ok {
							continue
						}
						for _, ca := range sum[g] {
							if ca.idx != j {
								continue
							}
							full := append(clonePath(pfx), ca.path...)
							verb := "reads"
							if ca.write {
								verb = "writes"
							}
							note(r, full, ca.write, true,
								fmt.Sprintf("goroutine `go %s(...)` %s `%s`", g, verb, r))
						}
					}
				case *IndexAssign:
					if r, p, ok := placeOf(st.Target); ok {
						note(r, p, true, false, fmt.Sprintf("this thread writes `%s`", r))
					}
				case *FieldAssign:
					if r, p, ok := placeOf(st.Target); ok {
						note(r, p, true, false, fmt.Sprintf("this thread writes `%s`", r))
					}
				case *ExprStmt:
					noteReads(st.X, note)
				case *AssignStmt:
					noteReads(st.Val, note)
				case *ReturnStmt:
					for _, e := range st.Vals {
						noteReads(e, note)
					}
				case *IfStmt:
					noteReads(st.Cond, note)
					visit(st.Then)
					visit(st.Else)
				case *WhileStmt:
					noteReads(st.Cond, note)
					visit(st.Body)
				case *RangeStmt:
					noteReads(st.X, note)
					visit(st.Body)
				case *ArenaStmt:
					visit(st.Body)
				}
			}
		}
		visit(f.Body)

		// a race: a root with >=2 concurrent accessors, >=1 a goroutine, >=1 a write
		for root, as := range accs {
			goCount, writeCount := 0, 0
			for _, a := range as {
				if a.byGo {
					goCount++
				}
				if a.write {
					writeCount++
				}
			}
			if goCount >= 1 && len(as) >= 2 && writeCount >= 1 {
				kind := "read/write"
				if writeCount >= 2 {
					kind = "write/write"
				}
				descs := make([]string, len(as))
				for i, a := range as {
					descs[i] = a.desc
				}
				sort.Strings(descs)
				key := f.Name + "|" + root + "|" + strings.Join(descs, "|")
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, raceFinding{Root: root, Decl: f.Name, Kind: kind, Writers: descs})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Decl != out[j].Decl {
			return out[i].Decl < out[j].Decl
		}
		return out[i].Root < out[j].Root
	})
	return out
}

// noteReads records every shared element/field READ inside an expression.
func noteReads(e Expr, note func(root string, path accPath, write, byGo bool, desc string)) {
	switch t := e.(type) {
	case nil:
		return
	case *Index:
		if r, p, ok := placeOf(t); ok {
			note(r, p, false, false, fmt.Sprintf("this thread reads `%s`", r))
		}
		noteReads(t.X, note)
		noteReads(t.Idx, note)
	case *FieldAccess:
		if r, p, ok := placeOf(t); ok {
			note(r, p, false, false, fmt.Sprintf("this thread reads `%s`", r))
		}
		noteReads(t.X, note)
	case *Call:
		for _, a := range t.Args {
			noteReads(a, note)
		}
	case *Binary:
		noteReads(t.L, note)
		noteReads(t.R, note)
	case *Unary:
		noteReads(t.X, note)
	case *SliceLit:
		for _, el := range t.Elems {
			noteReads(el, note)
		}
	case *StructLit:
		for _, v := range t.Vals {
			noteReads(v, note)
		}
	}
}

// toDiagnostic renders a finding as a check.go Diagnostic (Slice 1.5 will extend
// the code taxonomy and JSON surface; option (a): errors in `machin check`).
func (rf raceFinding) toDiagnostic() Diagnostic {
	code := "RACE002" // read/write
	if rf.Kind == "write/write" {
		code = "RACE001"
	}
	return Diagnostic{
		Severity: "error",
		Phase:    "race",
		Code:     code,
		Message:  fmt.Sprintf("data race on `%s` (%s): %s", rf.Root, rf.Kind, strings.Join(rf.Writers, "; ")),
		Decl:     rf.Decl,
	}
}
