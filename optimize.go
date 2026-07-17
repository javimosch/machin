package main

// optimize.go — provable superoptimization, Slice 2: `machin optimize <file>`.
//
// The optimizer proposes semantics-preserving rewrites (constant folding, algebraic identity
// elimination, strength reduction) and GATES every one through the bounded equivalence oracle
// (equiv.go). A rewrite is kept only if the oracle proves the rewritten function agrees with
// the original on the whole bounded input domain; a rewrite the oracle refutes is reported with
// the exact input that would change — so the optimizer can be speculative and stay honest.
//
// This is what makes it "provable": unlike a classic optimizer whose rules you must trust, here
// each applied rewrite carries an equivalence verdict (equivalent / equivalent-bounded), and an
// unsound rule (e.g. lowering signed /2^k to an arithmetic shift, wrong for negatives) is caught
// and dropped rather than silently miscompiling.

import (
	"encoding/json"
	"fmt"
	"strings"
)

type optPass struct {
	name string
	rule func(Expr) Expr
}

type optPassResult struct {
	Rule    string `json:"rule"`
	Verdict string `json:"verdict"`          // equivalent | equivalent-bounded | diverges | inconclusive
	Detail  string `json:"detail,omitempty"` // for diverges: the counterexample
}

type optFuncResult struct {
	Fn        string          `json:"fn"`
	Passes    []optPassResult `json:"passes,omitempty"`
	Optimized string          `json:"optimized,omitempty"` // rendered optimized function (if any pass applied)
	Note      string          `json:"note,omitempty"`      // e.g. "unbounded domain"
	applied   int
	rejected  int
}

type optReport struct {
	Funcs    []optFuncResult `json:"funcs"`
	Applied  int             `json:"applied"`  // total accepted rewrites
	Rejected int             `json:"rejected"` // total oracle-refuted rewrites
}

// the rewrite catalog, applied in order and cumulatively (each builds on the last accepted).
func optPasses() []optPass {
	return []optPass{
		{"constant-folding", ruleConstFold},
		{"algebraic-identity", ruleIdentity},
		{"strength-reduction", ruleStrengthMul},
		{"strength-reduction-div", ruleStrengthDiv}, // speculative: unsound for signed operands — the oracle is the gate
	}
}

// ---- rewrite rules (each runs on a node whose children are already rewritten) ----

func log2Pow2(n int64) (int, bool) {
	if n <= 0 || n&(n-1) != 0 {
		return 0, false
	}
	k := 0
	for n > 1 {
		n >>= 1
		k++
	}
	return k, true
}

func asInt(e Expr) (int64, bool) {
	l, ok := e.(*IntLit)
	if !ok {
		return 0, false
	}
	return l.Val, true
}

// isPure: evaluating e has no side effect and cannot trap, so dropping or duplicating it is safe.
func isPure(e Expr) bool {
	switch x := e.(type) {
	case *IntLit, *FloatLit, *BoolLit, *StringLit, *Ident:
		return true
	case *Unary:
		return isPure(x.X)
	case *Binary:
		// division/modulo can trap on a zero divisor — not pure
		if x.Op == "/" || x.Op == "%" {
			return false
		}
		return isPure(x.L) && isPure(x.R)
	}
	return false // Call, Index, FieldAccess, Recv, ...
}

func ruleConstFold(e Expr) Expr {
	b, ok := e.(*Binary)
	if !ok {
		return e
	}
	l, lo := asInt(b.L)
	r, ro := asInt(b.R)
	if !lo || !ro {
		return e
	}
	switch b.Op {
	case "+":
		return &IntLit{l + r}
	case "-":
		return &IntLit{l - r}
	case "*":
		return &IntLit{l * r}
	case "/":
		if r == 0 {
			return e
		}
		return &IntLit{l / r}
	case "%":
		if r == 0 {
			return e
		}
		return &IntLit{l % r}
	case "<<":
		if r < 0 || r >= 64 {
			return e
		}
		return &IntLit{l << uint(r)}
	case ">>":
		if r < 0 || r >= 64 {
			return e
		}
		return &IntLit{l >> uint(r)}
	case "&":
		return &IntLit{l & r}
	case "|":
		return &IntLit{l | r}
	case "^":
		return &IntLit{l ^ r}
	case "==":
		return &BoolLit{l == r}
	case "!=":
		return &BoolLit{l != r}
	case "<":
		return &BoolLit{l < r}
	case "<=":
		return &BoolLit{l <= r}
	case ">":
		return &BoolLit{l > r}
	case ">=":
		return &BoolLit{l >= r}
	}
	return e
}

func ruleIdentity(e Expr) Expr {
	b, ok := e.(*Binary)
	if !ok {
		return e
	}
	isZero := func(x Expr) bool { v, o := asInt(x); return o && v == 0 }
	isOne := func(x Expr) bool { v, o := asInt(x); return o && v == 1 }
	sameIdent := func(a, c Expr) bool {
		ai, ao := a.(*Ident)
		ci, co := c.(*Ident)
		return ao && co && ai.Name == ci.Name
	}
	switch b.Op {
	case "+":
		if isZero(b.R) {
			return b.L
		}
		if isZero(b.L) {
			return b.R
		}
	case "-":
		if isZero(b.R) {
			return b.L
		}
		if sameIdent(b.L, b.R) {
			return &IntLit{0} // x - x
		}
	case "*":
		if isOne(b.R) {
			return b.L
		}
		if isOne(b.L) {
			return b.R
		}
		if isZero(b.R) && isPure(b.L) {
			return &IntLit{0}
		}
		if isZero(b.L) && isPure(b.R) {
			return &IntLit{0}
		}
	case "/":
		if isOne(b.R) {
			return b.L
		}
	}
	return e
}

func ruleStrengthMul(e Expr) Expr {
	b, ok := e.(*Binary)
	if !ok || b.Op != "*" {
		return e
	}
	if v, o := asInt(b.R); o {
		if k, p := log2Pow2(v); p && k > 0 {
			return &Binary{Op: "<<", L: b.L, R: &IntLit{int64(k)}}
		}
	}
	if v, o := asInt(b.L); o {
		if k, p := log2Pow2(v); p && k > 0 {
			return &Binary{Op: "<<", L: b.R, R: &IntLit{int64(k)}}
		}
	}
	return e
}

func ruleStrengthDiv(e Expr) Expr {
	b, ok := e.(*Binary)
	if !ok || b.Op != "/" {
		return e
	}
	if v, o := asInt(b.R); o {
		if k, p := log2Pow2(v); p && k > 0 {
			return &Binary{Op: ">>", L: b.L, R: &IntLit{int64(k)}}
		}
	}
	return e
}

// ---- bottom-up AST transform (also serves as a deep clone under the identity rule) ----

func transformExpr(e Expr, rule func(Expr) Expr) Expr {
	switch x := e.(type) {
	case *Binary:
		return rule(&Binary{Op: x.Op, L: transformExpr(x.L, rule), R: transformExpr(x.R, rule)})
	case *Unary:
		return rule(&Unary{Op: x.Op, X: transformExpr(x.X, rule)})
	case *Call:
		return rule(&Call{Callee: x.Callee, Args: transformExprs(x.Args, rule), Spread: x.Spread})
	case *CallValue:
		return rule(&CallValue{Fn: transformExpr(x.Fn, rule), Args: transformExprs(x.Args, rule)})
	case *Index:
		return rule(&Index{X: transformExpr(x.X, rule), Idx: transformExpr(x.Idx, rule)})
	case *FieldAccess:
		return rule(&FieldAccess{X: transformExpr(x.X, rule), Name: x.Name})
	case *SliceLit:
		return rule(&SliceLit{Elem: x.Elem, Elems: transformExprs(x.Elems, rule)})
	case *StructLit:
		return rule(&StructLit{Type: x.Type, FieldNames: x.FieldNames, Vals: transformExprs(x.Vals, rule)})
	case *Recv:
		return rule(&Recv{Ch: transformExpr(x.Ch, rule)})
	default:
		return rule(e) // leaf: IntLit/FloatLit/StringLit/BoolLit/NilLit/Ident/Make*/FuncLit
	}
}

func transformExprs(es []Expr, rule func(Expr) Expr) []Expr {
	out := make([]Expr, len(es))
	for i, e := range es {
		out[i] = transformExpr(e, rule)
	}
	return out
}

func transformStmts(ss []Stmt, rule func(Expr) Expr) []Stmt {
	out := make([]Stmt, len(ss))
	for i, s := range ss {
		out[i] = transformStmt(s, rule)
	}
	return out
}

func transformStmt(s Stmt, rule func(Expr) Expr) Stmt {
	switch x := s.(type) {
	case *ExprStmt:
		return &ExprStmt{X: transformExpr(x.X, rule)}
	case *AssignStmt:
		return &AssignStmt{Name: x.Name, Op: x.Op, Val: transformExpr(x.Val, rule)}
	case *MultiAssign:
		return &MultiAssign{Names: x.Names, Op: x.Op, Rhs: transformExprs(x.Rhs, rule)}
	case *ReturnStmt:
		return &ReturnStmt{Vals: transformExprs(x.Vals, rule)}
	case *IfStmt:
		return &IfStmt{Cond: transformExpr(x.Cond, rule), Then: transformStmts(x.Then, rule), Else: transformStmts(x.Else, rule)}
	case *WhileStmt:
		return &WhileStmt{Cond: transformExpr(x.Cond, rule), Body: transformStmts(x.Body, rule)}
	case *RangeStmt:
		return &RangeStmt{Key: x.Key, Val: x.Val, X: transformExpr(x.X, rule), Body: transformStmts(x.Body, rule)}
	case *IndexAssign:
		return &IndexAssign{Target: &Index{X: transformExpr(x.Target.X, rule), Idx: transformExpr(x.Target.Idx, rule)}, Val: transformExpr(x.Val, rule)}
	case *FieldAssign:
		return &FieldAssign{Target: &FieldAccess{X: transformExpr(x.Target.X, rule), Name: x.Target.Name}, Val: transformExpr(x.Val, rule)}
	case *SendStmt:
		return &SendStmt{Ch: transformExpr(x.Ch, rule), Val: transformExpr(x.Val, rule)}
	case *ArenaStmt:
		return &ArenaStmt{Body: transformStmts(x.Body, rule)}
	default:
		return s // Break/Continue/Go/Select — not rewritten (their exprs aren't arithmetic targets here)
	}
}

func cloneFunc(f *FuncDecl) *FuncDecl {
	nf := *f
	nf.Body = transformStmts(f.Body, func(e Expr) Expr { return e })
	return &nf
}

// ---- the oracle-gated optimizer ----

func optimizeFunc(orig *FuncDecl, verify func(cand *FuncDecl) equivResult) optFuncResult {
	r := optFuncResult{Fn: orig.Name}
	work := cloneFunc(orig)
	for _, pass := range optPasses() {
		cand := cloneFunc(work)
		cand.Body = transformStmts(cand.Body, pass.rule)
		cand.Name = orig.Name + "$opt"
		if renderStmtsSrc(cand.Body) == renderStmtsSrc(work.Body) {
			continue // this pass changed nothing
		}
		v := verify(cand)
		switch v.Verdict {
		case "equivalent", "equivalent-bounded":
			work = cand
			r.applied++
			r.Passes = append(r.Passes, optPassResult{Rule: pass.name, Verdict: v.Verdict})
		case "diverges":
			r.rejected++
			r.Passes = append(r.Passes, optPassResult{Rule: pass.name, Verdict: "diverges",
				Detail: fmt.Sprintf("%s(%s) would change from %s to %s", orig.Name, v.Bind, v.FVal, v.GVal)})
		default:
			// inconclusive — can't certify this rewrite (unmodeled body); skip it, don't apply
			r.Passes = append(r.Passes, optPassResult{Rule: pass.name, Verdict: v.Verdict})
		}
	}
	if r.applied > 0 {
		work.Name = orig.Name
		if src, complete := renderFuncSrc(work); complete {
			r.Optimized = src
		}
	}
	return r
}

// optimizeProgram runs the optimizer over every instantiated, bounded-domain function.
func optimizeProgram(prog *Program, c *Checker) optReport {
	funcs := map[string]*FuncDecl{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	var rep optReport
	seen := map[string]bool{}
	for _, inst := range c.reps {
		fFn := c.instFn[inst]
		if fFn == nil || fFn.Name == "main" || seen[fFn.Name] {
			continue
		}
		seen[fFn.Name] = true
		domains, prov, ok := paramDomains(c, inst, len(fFn.Params))
		if !ok {
			rep.Funcs = append(rep.Funcs, optFuncResult{Fn: fFn.Name, Note: "unbounded param domain — not optimizable"})
			continue
		}
		orig := fFn
		verify := func(cand *FuncDecl) equivResult {
			return equivOverDomains(orig.Name, cand.Name, orig, cand, domains, prov, funcs, c.structs)
		}
		res := optimizeFunc(orig, verify)
		rep.Applied += res.applied
		rep.Rejected += res.rejected
		if len(res.Passes) > 0 || res.Note != "" {
			rep.Funcs = append(rep.Funcs, res)
		}
	}
	return rep
}

// ---- CLI ----

func cmdOptimize(args []string) error {
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
		return fmt.Errorf("usage: machin optimize [--json] <file...> — propose + prove faster equivalent code")
	}
	var combined strings.Builder
	for _, p := range files {
		data, err := readModule(p)
		if err != nil {
			return fmt.Errorf("optimize: %w", err)
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}
	blocks, _, err := splitFunctionsLoc(combined.String())
	if err != nil {
		return fmt.Errorf("optimize: %w", err)
	}
	decls := make([]string, len(blocks))
	for i, b := range blocks {
		decls[i] = normalize(b)
	}
	prog, perr := ParseProgram(decls)
	if perr != nil {
		return fmt.Errorf("optimize: parse: %w", perr)
	}
	c, cerr := Check(prog)
	if cerr != nil {
		return fmt.Errorf("optimize: %w (optimize needs a program that typechecks)", cerr)
	}

	rep := optimizeProgram(prog, c)
	if jsonOut {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		printOptReport(rep)
	}
	return nil
}

func printOptReport(rep optReport) {
	any := false
	for _, f := range rep.Funcs {
		if len(f.Passes) == 0 {
			continue
		}
		any = true
		fmt.Printf("  %s:\n", f.Fn)
		for _, p := range f.Passes {
			switch p.Verdict {
			case "equivalent", "equivalent-bounded":
				fmt.Printf("    ✓ %-24s proven %s\n", p.Rule, p.Verdict)
			case "diverges":
				fmt.Printf("    ✗ %-24s REJECTED by the oracle — %s\n", p.Rule, p.Detail)
			default:
				fmt.Printf("    · %-24s skipped (%s — not certifiable)\n", p.Rule, p.Verdict)
			}
		}
		if f.Optimized != "" {
			fmt.Printf("    optimized: %s\n", f.Optimized)
		}
	}
	if !any {
		fmt.Println("  no rewrites applied (no arithmetic simplification found within bounds)")
	}
	fmt.Printf("\n%d rewrite(s) proven equivalent, %d rejected by the oracle\n", rep.Applied, rep.Rejected)
}
