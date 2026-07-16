package main

// falsify.go — "The Falsifier", Slice 1.1.
//
// A bounded, UNSOUND-COMPLETE bug finder over the typed IR: it enumerates small
// concrete inputs to each function and reports the exact input that makes a
// runtime-checked property fail (index out of range, divide/modulo by zero). It
// finds bugs; it does NOT prove their absence — so findings are advisory
// diagnostics and must never hard-fail `build` (see check.go integration).
//
// Reporting is conservative by construction: a counterexample is emitted ONLY
// when a fully-modeled concrete path reaches the trap. The instant evaluation
// touches something the interpreter cannot model (an unknown call, an
// unsupported node), that input is marked `unknown` and never reported — so a
// stubbed value can never manufacture a false positive.
//
// Slice 1.1 scope: int / float / bool / string / []int params; arithmetic,
// comparisons, if / while / for-range, index, len, arena. Structs, maps and
// interprocedural inlining arrive in Slice 1.3.

import (
	"fmt"
	"sort"
	"strings"
)

// ---- bound policy (auditable in one place; logged by the verdict envelope) ----

const (
	falsSliceLenMax = 3      // enumerate slice lengths 0..falsSliceLenMax
	falsInputCap    = 200000 // max concrete input tuples per function
	falsStepBudget  = 200000 // max interpreter steps per input (loop guard)
)

var (
	falsIntDomain     = []int64{0, 1, -1, 2, 3}
	falsFloatDomain   = []float64{0, 1, -1, 2}
	falsBoolDomain    = []bool{false, true}
	falsStringDomain  = []string{"", "a", "ab"}
	falsSliceElemVals = []int64{0, 1, 2}
)

// ---- finding ----

type falsifyFinding struct {
	Decl string // source function the bug lives in
	Code string // FALS001 (index OOB) | FALS002 (div/mod by zero)
	Prop string // human property, e.g. "index out of range"
	Expr string // rendered offending expression
	Bind string // "xs=[]int{}, i=2" — the concrete counterexample
	args []fval // the failing input, for repro generation (not serialized)
}

func (ff falsifyFinding) toDiagnostic() Diagnostic {
	return Diagnostic{
		Severity: "warning", // advisory: falsify findings never fail `check`/`build`
		Phase:    "falsify",
		Code:     ff.Code,
		Message:  fmt.Sprintf("%s at `%s` when %s", ff.Prop, ff.Expr, ff.Bind),
		Decl:     ff.Decl,
	}
}

// ---- concrete values ----

type fval struct {
	k  Kind
	i  int64
	f  float64
	b  bool
	s  string
	sl []fval
}

func vint(i int64) fval     { return fval{k: KInt, i: i} }
func vfloat(f float64) fval { return fval{k: KFloat, f: f} }
func vbool(b bool) fval     { return fval{k: KBool, b: b} }
func vstr(s string) fval    { return fval{k: KString, s: s} }

func (v fval) truthy() bool { return v.b }

func (v fval) String() string {
	switch v.k {
	case KInt:
		return fmt.Sprintf("%d", v.i)
	case KFloat:
		return fmt.Sprintf("%g", v.f)
	case KBool:
		return fmt.Sprintf("%v", v.b)
	case KString:
		return fmt.Sprintf("%q", v.s)
	case KSlice:
		parts := make([]string, len(v.sl))
		for i, e := range v.sl {
			parts[i] = e.String()
		}
		return "[]int{" + strings.Join(parts, ", ") + "}"
	}
	return "?"
}

// ---- interpreter control signals ----

// A property violation (counterexample): reported.
type fviol struct {
	prop, expr string
}

// The interpreter reached something it cannot model. That input is inconclusive;
// it is NEVER reported (this is what keeps the pass free of false positives).
type funknown struct{ why string }

type ctrl int

const (
	ctrlNone ctrl = iota
	ctrlBreak
	ctrlContinue
	ctrlReturn
)

type interp struct {
	env   map[string]fval
	funcs map[string]*FuncDecl // for len() only in 1.1; user calls -> unknown
	steps int
}

func (ip *interp) fail(prop, expr string) { panic(fviol{prop, expr}) }
func (ip *interp) unknown(why string)     { panic(funknown{why}) }

func (ip *interp) tick() {
	ip.steps++
	if ip.steps > falsStepBudget {
		ip.unknown("step budget")
	}
}

func (ip *interp) evalStmts(stmts []Stmt) ctrl {
	for _, st := range stmts {
		if c := ip.evalStmt(st); c != ctrlNone {
			return c
		}
	}
	return ctrlNone
}

func (ip *interp) evalStmt(st Stmt) ctrl {
	ip.tick()
	switch s := st.(type) {
	case *AssignStmt:
		ip.env[s.Name] = ip.eval(s.Val)
	case *ExprStmt:
		ip.eval(s.X)
	case *ReturnStmt:
		for _, v := range s.Vals {
			ip.eval(v)
		}
		return ctrlReturn
	case *BreakStmt:
		return ctrlBreak
	case *ContinueStmt:
		return ctrlContinue
	case *IfStmt:
		if ip.eval(s.Cond).truthy() {
			return ip.evalStmts(s.Then)
		}
		return ip.evalStmts(s.Else)
	case *WhileStmt:
		for ip.eval(s.Cond).truthy() {
			ip.tick()
			c := ip.evalStmts(s.Body)
			if c == ctrlBreak {
				break
			}
			if c == ctrlReturn {
				return ctrlReturn
			}
		}
	case *RangeStmt:
		return ip.evalRange(s)
	case *IndexAssign:
		base := ip.eval(s.Target.X)
		idx := ip.eval(s.Target.Idx)
		ip.checkIndex(base, idx, s.Target)
		v := ip.eval(s.Val)
		if id, ok := s.Target.X.(*Ident); ok && base.k == KSlice {
			base.sl[idx.i] = v
			ip.env[id.Name] = base
		}
	case *ArenaStmt:
		return ip.evalStmts(s.Body) // allocation is transparent to interpretation
	default:
		ip.unknown(fmt.Sprintf("stmt %T", st))
	}
	return ctrlNone
}

func (ip *interp) evalRange(s *RangeStmt) ctrl {
	base := ip.eval(s.X)
	n := 0
	switch base.k {
	case KSlice:
		n = len(base.sl)
	case KString:
		n = len(base.s)
	default:
		ip.unknown("range over " + kindName(base.k))
	}
	for idx := 0; idx < n; idx++ {
		ip.tick()
		if s.Key != "" && s.Key != "_" {
			ip.env[s.Key] = vint(int64(idx))
		}
		if s.Val != "" && s.Val != "_" {
			if base.k == KSlice {
				ip.env[s.Val] = base.sl[idx]
			} else {
				ip.env[s.Val] = vstr(string(base.s[idx])) // char is a 1-rune string
			}
		}
		c := ip.evalStmts(s.Body)
		if c == ctrlBreak {
			break
		}
		if c == ctrlReturn {
			return ctrlReturn
		}
	}
	return ctrlNone
}

func (ip *interp) checkIndex(base, idx fval, node *Index) {
	var n int64
	switch base.k {
	case KSlice:
		n = int64(len(base.sl))
	case KString:
		n = int64(len(base.s))
	default:
		ip.unknown("index into " + kindName(base.k))
	}
	if idx.i < 0 || idx.i >= n {
		ip.fail("index out of range", exprStr(node))
	}
}

func (ip *interp) eval(e Expr) fval {
	ip.tick()
	switch x := e.(type) {
	case *IntLit:
		return vint(x.Val)
	case *FloatLit:
		return vfloat(x.Val)
	case *BoolLit:
		return vbool(x.Val)
	case *StringLit:
		return vstr(x.Val)
	case *Ident:
		v, ok := ip.env[x.Name]
		if !ok {
			ip.unknown("free ident " + x.Name)
		}
		return v
	case *Unary:
		v := ip.eval(x.X)
		switch x.Op {
		case "-":
			if v.k == KFloat {
				return vfloat(-v.f)
			}
			return vint(-v.i)
		case "!":
			return vbool(!v.b)
		}
		ip.unknown("unary " + x.Op)
	case *Binary:
		return ip.evalBinary(x)
	case *Index:
		base := ip.eval(x.X)
		idx := ip.eval(x.Idx)
		ip.checkIndex(base, idx, x)
		if base.k == KSlice {
			return base.sl[idx.i]
		}
		return vstr(string(base.s[idx.i]))
	case *Call:
		if x.Callee == "len" && len(x.Args) == 1 {
			v := ip.eval(x.Args[0])
			switch v.k {
			case KSlice:
				return vint(int64(len(v.sl)))
			case KString:
				return vint(int64(len(v.s)))
			}
			ip.unknown("len of " + kindName(v.k))
		}
		// any other call (user func or unmodeled builtin) -> inconclusive.
		ip.unknown("call " + x.Callee)
	}
	ip.unknown(fmt.Sprintf("expr %T", e))
	return fval{} // unreachable
}

func (ip *interp) evalBinary(x *Binary) fval {
	l := ip.eval(x.L)
	if x.Op == "&&" {
		if !l.b {
			return vbool(false)
		}
		return vbool(ip.eval(x.R).b)
	}
	if x.Op == "||" {
		if l.b {
			return vbool(true)
		}
		return vbool(ip.eval(x.R).b)
	}
	r := ip.eval(x.R)
	if l.k == KFloat || r.k == KFloat {
		lf, rf := l.f, r.f
		if l.k == KInt {
			lf = float64(l.i)
		}
		if r.k == KInt {
			rf = float64(r.i)
		}
		switch x.Op {
		case "+":
			return vfloat(lf + rf)
		case "-":
			return vfloat(lf - rf)
		case "*":
			return vfloat(lf * rf)
		case "/":
			if rf == 0 {
				ip.fail("division by zero", exprStr(x))
			}
			return vfloat(lf / rf)
		case "<":
			return vbool(lf < rf)
		case "<=":
			return vbool(lf <= rf)
		case ">":
			return vbool(lf > rf)
		case ">=":
			return vbool(lf >= rf)
		case "==":
			return vbool(lf == rf)
		case "!=":
			return vbool(lf != rf)
		}
		ip.unknown("float op " + x.Op)
	}
	if l.k == KString {
		switch x.Op {
		case "+":
			return vstr(l.s + r.s)
		case "==":
			return vbool(l.s == r.s)
		case "!=":
			return vbool(l.s != r.s)
		case "<":
			return vbool(l.s < r.s)
		case ">":
			return vbool(l.s > r.s)
		case "<=":
			return vbool(l.s <= r.s)
		case ">=":
			return vbool(l.s >= r.s)
		}
		ip.unknown("string op " + x.Op)
	}
	li, ri := l.i, r.i
	switch x.Op {
	case "+":
		return vint(li + ri)
	case "-":
		return vint(li - ri)
	case "*":
		return vint(li * ri)
	case "/":
		if ri == 0 {
			ip.fail("division by zero", exprStr(x))
		}
		return vint(li / ri)
	case "%":
		if ri == 0 {
			ip.fail("division by zero", exprStr(x))
		}
		return vint(li % ri)
	case "&":
		return vint(li & ri)
	case "|":
		return vint(li | ri)
	case "^":
		return vint(li ^ ri)
	case "<<":
		return vint(li << uint(ri))
	case ">>":
		return vint(li >> uint(ri))
	case "<":
		return vbool(li < ri)
	case "<=":
		return vbool(li <= ri)
	case ">":
		return vbool(li > ri)
	case ">=":
		return vbool(li >= ri)
	case "==":
		return vbool(li == ri)
	case "!=":
		return vbool(li != ri)
	}
	ip.unknown("int op " + x.Op)
	return fval{}
}

func kindName(k Kind) string {
	switch k {
	case KInt:
		return "int"
	case KFloat:
		return "float"
	case KBool:
		return "bool"
	case KString:
		return "string"
	case KSlice:
		return "slice"
	case KStruct:
		return "struct"
	case KMap:
		return "map"
	}
	return "?"
}

// ---- expression rendering (precedence-correct; AST carries no positions) ----

// binPrec is the binding tightness of a binary op (higher binds tighter). Used
// to parenthesize children only when needed, so `100 % (a - b)` renders with its
// parens instead of the ambiguous `100 % a - b`.
func binPrec(op string) int {
	switch op {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=", "<", "<=", ">", ">=":
		return 3
	case "+", "-", "|", "^":
		return 4
	case "*", "/", "%", "&", "<<", ">>":
		return 5
	}
	return 0
}

func exprStr(e Expr) string { return exprPrec(e, 0) }

func exprPrec(e Expr, parent int) string {
	switch x := e.(type) {
	case *IntLit:
		return fmt.Sprintf("%d", x.Val)
	case *FloatLit:
		return fmt.Sprintf("%g", x.Val)
	case *BoolLit:
		return fmt.Sprintf("%v", x.Val)
	case *StringLit:
		return fmt.Sprintf("%q", x.Val)
	case *Ident:
		return x.Name
	case *Unary:
		return x.Op + exprPrec(x.X, 6)
	case *Binary:
		p := binPrec(x.Op)
		s := exprPrec(x.L, p) + " " + x.Op + " " + exprPrec(x.R, p+1)
		if p < parent {
			return "(" + s + ")"
		}
		return s
	case *Index:
		return exprPrec(x.X, 7) + "[" + exprStr(x.Idx) + "]"
	case *Call:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = exprStr(a)
		}
		return x.Callee + "(" + strings.Join(args, ", ") + ")"
	case *FieldAccess:
		return exprPrec(x.X, 7) + "." + x.Name
	}
	return "?"
}

// ---- bounded input enumeration ----

// domain returns the candidate values for a param of the given slot's kind, or
// (nil, false) when the type is out of Slice 1.1 scope.
func (c *Checker) domain(slot int) ([]fval, bool) {
	switch c.kindOf(slot) {
	case KInt:
		out := make([]fval, len(falsIntDomain))
		for i, v := range falsIntDomain {
			out[i] = vint(v)
		}
		return out, true
	case KBool:
		return []fval{vbool(false), vbool(true)}, true
	case KFloat:
		out := make([]fval, len(falsFloatDomain))
		for i, v := range falsFloatDomain {
			out[i] = vfloat(v)
		}
		return out, true
	case KString:
		out := make([]fval, len(falsStringDomain))
		for i, v := range falsStringDomain {
			out[i] = vstr(v)
		}
		return out, true
	case KSlice:
		if c.kindOf(c.elem[c.find(slot)]) != KInt {
			return nil, false // 1.1: only []int
		}
		var out []fval
		for n := 0; n <= falsSliceLenMax; n++ {
			for _, combo := range gen(n, falsSliceElemVals) {
				sl := make([]fval, n)
				for i, v := range combo {
					sl[i] = vint(v)
				}
				out = append(out, fval{k: KSlice, sl: sl})
			}
		}
		return out, true
	}
	return nil, false
}

func gen(n int, vals []int64) [][]int64 {
	if n == 0 {
		return [][]int64{{}}
	}
	rest := gen(n-1, vals)
	var out [][]int64
	for _, v := range vals {
		for _, r := range rest {
			out = append(out, append([]int64{v}, r...))
		}
	}
	return out
}

// ---- the pass ----

// falsifyStats reports coverage so callers can distinguish "checked, clean" from
// "couldn't check". Slice 1.2 surfaces this as the per-function verdict envelope.
type falsifyStats struct {
	Checked    int // functions fully enumerated (found or clean)
	Skipped    int // functions with an out-of-scope param type
	AllUnknown int // functions where every input was inconclusive
}

// detectFalsifiable bounded-checks every representative instance and returns the
// counterexamples found, sorted deterministically. Mirrors detectRaces.
func detectFalsifiable(prog *Program, c *Checker) []falsifyFinding {
	out, _ := detectFalsifiableStats(prog, c)
	return out
}

func detectFalsifiableStats(prog *Program, c *Checker) ([]falsifyFinding, falsifyStats) {
	funcs := map[string]*FuncDecl{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	var out []falsifyFinding
	var st falsifyStats
	seen := map[string]bool{} // dedup (Decl|Code|Expr) across monomorphized instances

	for _, inst := range c.reps {
		fn := c.instFn[inst]
		if fn == nil {
			continue
		}
		slots := c.funcParam[inst]

		domains := make([][]fval, len(fn.Params))
		scoped := true
		for i, slot := range slots {
			d, ok := c.domain(slot)
			if !ok {
				scoped = false
				break
			}
			domains[i] = d
		}
		if !scoped {
			st.Skipped++
			continue
		}

		ff, verdict := falsifyOne(fn, domains, funcs)
		switch verdict {
		case "counterexample":
			st.Checked++
			key := ff.Decl + "|" + ff.Code + "|" + ff.Expr
			if !seen[key] {
				seen[key] = true
				out = append(out, ff)
			}
		case "clean":
			st.Checked++
		case "unknown":
			st.AllUnknown++
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Decl != out[j].Decl {
			return out[i].Decl < out[j].Decl
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Expr < out[j].Expr
	})
	return out, st
}

// falsifyOne enumerates one function. Returns ("counterexample", finding) on the
// first fully-modeled trap, ("clean", _) if all inputs ran to completion without
// tripping, or ("unknown", _) if every input was inconclusive / the cap was hit
// before any conclusive result.
func falsifyOne(fn *FuncDecl, domains [][]fval, funcs map[string]*FuncDecl) (falsifyFinding, string) {
	idx := make([]int, len(domains))
	tried := 0
	sawConclusive := false
	for {
		if tried >= falsInputCap {
			break
		}
		args := make([]fval, len(domains))
		for i := range domains {
			args[i] = domains[i][idx[i]]
		}
		tried++
		viol, unknown := runOne(fn, args, funcs)
		if viol != nil {
			bind := bindings(fn.Params, args)
			return falsifyFinding{
				Decl: fn.Name,
				Code: propCode(viol.prop),
				Prop: viol.prop,
				Expr: viol.expr,
				Bind: bind,
				args: args,
			}, "counterexample"
		}
		if !unknown {
			sawConclusive = true
		}
		// mixed-radix increment
		k := 0
		for ; k < len(idx); k++ {
			idx[k]++
			if idx[k] < len(domains[k]) {
				break
			}
			idx[k] = 0
		}
		if k == len(idx) {
			break
		}
	}
	if sawConclusive {
		return falsifyFinding{}, "clean"
	}
	return falsifyFinding{}, "unknown"
}

func propCode(prop string) string {
	if strings.HasPrefix(prop, "index") {
		return "FALS001"
	}
	return "FALS002" // division by zero
}

func bindings(names []string, args []fval) string {
	parts := make([]string, len(names))
	for i := range names {
		parts[i] = fmt.Sprintf("%s=%s", names[i], args[i])
	}
	return strings.Join(parts, ", ")
}

// runOne evaluates fn on one concrete tuple. Returns (violation, unknown): a
// non-nil violation is a fully-modeled counterexample; unknown means the path
// touched something unmodeled and is inconclusive (never reported).
func runOne(fn *FuncDecl, args []fval, funcs map[string]*FuncDecl) (v *fviol, unknown bool) {
	ip := &interp{env: map[string]fval{}, funcs: funcs}
	for i, p := range fn.Params {
		if i < len(args) {
			ip.env[p] = args[i]
		}
	}
	for _, r := range fn.Returns {
		ip.env[r] = vint(0)
	}
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case fviol:
				v = &e
			case funknown:
				unknown = true
			default:
				panic(r)
			}
		}
	}()
	ip.evalStmts(fn.Body)
	return nil, false
}

// reproProgram builds a runnable .mfl reproducing a counterexample: the original
// decls plus a main calling target with the failing literal args. Built with
// --safe it panics at exactly the offending op — the auto-promotable regression
// test. (Used by the `machin falsify --repro` driver in Slice 1.2 and the test.)
func reproProgram(decls []string, target string, bindArgs []fval) string {
	var b strings.Builder
	for _, d := range decls {
		if declName(d) == "main" {
			continue // the repro supplies its own main
		}
		b.WriteString(d)
		b.WriteString("\n")
	}
	parts := make([]string, len(bindArgs))
	for i := range bindArgs {
		parts[i] = bindArgs[i].String()
	}
	fmt.Fprintf(&b, "\nfunc main() {\n\tprintln(str(%s(%s)))\n}\n", target, strings.Join(parts, ", "))
	return b.String()
}
