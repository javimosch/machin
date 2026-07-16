package main

// falsify_spike.go — Phase 0 spike for "The Falsifier".
//
// De-risks one question: can a pure bounded checker over machin's EXISTING typed
// IR find a real bug (div-by-zero / index-out-of-bounds) in an MFL function and
// hand back a concrete failing input + a runnable repro, fast enough for an
// agent's edit loop?
//
// This is a THROWAWAY reference (Go, like racecheck.go was before the MFL port),
// not wired into main. It reuses ParseProgram + Check, reads per-instance param
// kinds from the checker, then bounded-enumerates concrete inputs and evaluates
// the function body with an interpreter that traps on div-by-zero and OOB.
//
// Scope kept deliberately narrow: int / float / bool / string / []int params;
// arithmetic, comparisons, if/while/for-range, index, len. Enough to prove the
// loop end to end. Structs / maps / calls-to-user-funcs are out of spike scope.

import (
	"fmt"
	"strings"
	"time"
)

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

// ---- violation signalling ----

type fviol struct {
	prop string // "division by zero", "index out of range"
	expr string // rendered offending expression
}

// control-flow signal out of a statement list
type ctrl int

const (
	ctrlNone ctrl = iota
	ctrlBreak
	ctrlContinue
	ctrlReturn
)

// counterexample
type cex struct {
	prop  string
	expr  string
	args  []fval
	names []string
}

func (c cex) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s at `%s`\n  when ", c.prop, c.expr)
	parts := make([]string, len(c.args))
	for i := range c.args {
		parts[i] = fmt.Sprintf("%s=%s", c.names[i], c.args[i])
	}
	b.WriteString(strings.Join(parts, ", "))
	return b.String()
}

// ---- the interpreter ----

type interp struct {
	env   map[string]fval
	viol  *fviol
	steps int // guards against unbounded loops
}

const maxSteps = 200000

func (ip *interp) fail(prop, expr string) {
	if ip.viol == nil {
		ip.viol = &fviol{prop: prop, expr: expr}
	}
	panic(ip.viol)
}

func (ip *interp) tick() {
	ip.steps++
	if ip.steps > maxSteps {
		panic("falsify:timeout")
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
		base := ip.eval(s.X)
		n := 0
		if base.k == KSlice {
			n = len(base.sl)
		} else if base.k == KString {
			n = len(base.s)
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
					ip.env[s.Val] = vint(int64(base.s[idx]))
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
	case *IndexAssign:
		base := ip.eval(s.Target.X)
		idx := ip.eval(s.Target.Idx)
		ip.checkIndex(base, idx, s.Target)
		v := ip.eval(s.Val)
		if id, ok := s.Target.X.(*Ident); ok && base.k == KSlice {
			base.sl[idx.i] = v
			ip.env[id.Name] = base
		}
	}
	return ctrlNone
}

func (ip *interp) checkIndex(base, idx fval, node *Index) {
	n := int64(0)
	if base.k == KSlice {
		n = int64(len(base.sl))
	} else if base.k == KString {
		n = int64(len(base.s))
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
		return ip.env[x.Name]
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
	case *Binary:
		return ip.evalBinary(x)
	case *Index:
		base := ip.eval(x.X)
		idx := ip.eval(x.Idx)
		ip.checkIndex(base, idx, x)
		if base.k == KSlice {
			return base.sl[idx.i]
		}
		return vint(int64(base.s[idx.i]))
	case *Call:
		if x.Callee == "len" && len(x.Args) == 1 {
			v := ip.eval(x.Args[0])
			if v.k == KSlice {
				return vint(int64(len(v.sl)))
			}
			return vint(int64(len(v.s)))
		}
		// unknown call: evaluate args, return zero (out of spike scope)
		for _, a := range x.Args {
			ip.eval(a)
		}
		return vint(0)
	}
	return vint(0)
}

func (ip *interp) evalBinary(x *Binary) fval {
	l := ip.eval(x.L)
	// short-circuit
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
	}
	if l.k == KString {
		switch x.Op {
		case "+":
			return vstr(l.s + r.s)
		case "==":
			return vbool(l.s == r.s)
		case "!=":
			return vbool(l.s != r.s)
		}
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
	return vint(0)
}

// ---- expression rendering (for counterexamples; no source positions in AST) ----

func exprStr(e Expr) string {
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
		return x.Op + exprStr(x.X)
	case *Binary:
		return exprStr(x.L) + " " + x.Op + " " + exprStr(x.R)
	case *Index:
		return exprStr(x.X) + "[" + exprStr(x.Idx) + "]"
	case *Call:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = exprStr(a)
		}
		return x.Callee + "(" + strings.Join(args, ", ") + ")"
	case *FieldAccess:
		return exprStr(x.X) + "." + x.Name
	}
	return "?"
}

// ---- bounded input enumeration ----

// domain returns the concrete candidate values for a param of the given kind.
func (c *Checker) domain(slot int) []fval {
	switch c.kindOf(slot) {
	case KInt:
		return []fval{vint(0), vint(1), vint(-1), vint(2), vint(3)}
	case KBool:
		return []fval{vbool(false), vbool(true)}
	case KFloat:
		return []fval{vfloat(0), vfloat(1), vfloat(-1), vfloat(2)}
	case KString:
		return []fval{vstr(""), vstr("a"), vstr("ab")}
	case KSlice:
		el := c.kindOf(c.elem[c.find(slot)])
		if el != KInt {
			return nil // spike: only []int
		}
		var out []fval
		// lengths 0..3, elements drawn from {0,1,2}
		fill := []int64{0, 1, 2}
		for n := 0; n <= 3; n++ {
			combos := gen(n, fill)
			for _, combo := range combos {
				sl := make([]fval, n)
				for i, v := range combo {
					sl[i] = vint(v)
				}
				out = append(out, fval{k: KSlice, sl: sl})
			}
		}
		return out
	}
	return nil
}

// gen returns all length-n tuples drawn from vals (bounded).
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

// ---- the falsifier ----

type falsifyResult struct {
	found   bool
	ce      cex
	tried   int
	elapsed time.Duration
	unknown bool // hit a timeout on some inputs
}

// falsify bounded-checks the function `target` in prog for div-by-zero / OOB.
func falsify(prog *Program, c *Checker, target string) falsifyResult {
	start := time.Now()
	// find a representative instance of the target function
	inst := ""
	for _, rep := range c.reps {
		if c.instFn[rep].Name == target {
			inst = rep
			break
		}
	}
	if inst == "" {
		return falsifyResult{elapsed: time.Since(start)}
	}
	fn := c.instFn[inst]
	slots := c.funcParam[inst]

	// build per-param domains
	domains := make([][]fval, len(fn.Params))
	for i, slot := range slots {
		domains[i] = c.domain(slot)
		if domains[i] == nil {
			return falsifyResult{elapsed: time.Since(start)} // unsupported param type
		}
	}

	var res falsifyResult
	// cartesian product over domains, capped
	const cap = 200000
	idx := make([]int, len(domains))
	for {
		if res.tried >= cap {
			break
		}
		args := make([]fval, len(domains))
		for i := range domains {
			args[i] = domains[i][idx[i]]
		}
		res.tried++
		if ce, unk := runOne(fn, args); unk {
			res.unknown = true
		} else if ce != nil {
			res.found = true
			res.ce = *ce
			res.ce.args = args
			res.ce.names = fn.Params
			res.elapsed = time.Since(start)
			return res
		}
		// increment mixed-radix counter
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
	res.elapsed = time.Since(start)
	return res
}

// runOne evaluates fn on one concrete arg tuple. Returns (counterexample, unknown).
func runOne(fn *FuncDecl, args []fval) (ce *cex, unknown bool) {
	ip := &interp{env: map[string]fval{}}
	for i, p := range fn.Params {
		ip.env[p] = args[i]
	}
	for _, r := range fn.Returns {
		ip.env[r] = vint(0)
	}
	defer func() {
		if r := recover(); r != nil {
			if v, ok := r.(*fviol); ok {
				ce = &cex{prop: v.prop, expr: v.expr}
				return
			}
			if s, ok := r.(string); ok && s == "falsify:timeout" {
				unknown = true
				return
			}
			panic(r)
		}
	}()
	ip.evalStmts(fn.Body)
	return nil, false
}

// reproProgram builds a runnable .mfl that triggers the counterexample: the
// original decls + a main calling target with the failing literal args. Built
// with --safe it panics at exactly the offending op.
func reproProgram(decls []string, target string, ce cex) string {
	var b strings.Builder
	for _, d := range decls {
		b.WriteString(d)
		b.WriteString("\n")
	}
	b.WriteString("\nfunc main() {\n")
	parts := make([]string, len(ce.args))
	for i := range ce.args {
		parts[i] = ce.args[i].String()
	}
	// consume the result (MFL has no `_ =` discard); str() forces evaluation.
	fmt.Fprintf(&b, "\tprintln(str(%s(%s)))\n}\n", target, strings.Join(parts, ", "))
	return b.String()
}
