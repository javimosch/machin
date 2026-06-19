package main

import (
	"fmt"
	"strings"
)

// Value is any runtime value: int64, float64, string, bool, or nil.
type Value interface{}

type Interp struct {
	funcs map[string]*FuncDecl
	out   *strings.Builder
}

// returnSignal is used to unwind the stack on a return statement.
type returnSignal struct{ val Value }

func NewInterp() *Interp {
	return &Interp{funcs: map[string]*FuncDecl{}, out: &strings.Builder{}}
}

func (in *Interp) Register(fn *FuncDecl) error {
	if _, ok := in.funcs[fn.Name]; ok {
		return fmt.Errorf("duplicate function %q", fn.Name)
	}
	in.funcs[fn.Name] = fn
	return nil
}

// Run executes main() and returns its captured stdout.
func (in *Interp) Run() (string, error) {
	main, ok := in.funcs["main"]
	if !ok {
		return "", fmt.Errorf("no main function defined")
	}
	if _, err := in.callFunc(main, nil); err != nil {
		return in.out.String(), err
	}
	return in.out.String(), nil
}

type scope struct {
	vars   map[string]Value
	parent *scope
}

func newScope(parent *scope) *scope {
	return &scope{vars: map[string]Value{}, parent: parent}
}

func (s *scope) get(name string) (Value, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if v, ok := cur.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

func (s *scope) set(name string, v Value) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.vars[name]; ok {
			cur.vars[name] = v
			return true
		}
	}
	return false
}

func (in *Interp) callFunc(fn *FuncDecl, args []Value) (Value, error) {
	if len(args) != len(fn.Params) {
		return nil, fmt.Errorf("%s: expected %d args, got %d", fn.Name, len(fn.Params), len(args))
	}
	sc := newScope(nil)
	for i, p := range fn.Params {
		sc.vars[p] = args[i]
	}
	ret, err := in.execBlock(fn.Body, sc)
	if err != nil {
		return nil, err
	}
	if rs, ok := ret.(returnSignal); ok {
		return rs.val, nil
	}
	return nil, nil
}

// execBlock returns a returnSignal (as Value) if a return happened, else nil.
func (in *Interp) execBlock(stmts []Stmt, sc *scope) (Value, error) {
	for _, s := range stmts {
		sig, err := in.execStmt(s, sc)
		if err != nil {
			return nil, err
		}
		if sig != nil {
			return sig, nil
		}
	}
	return nil, nil
}

func (in *Interp) execStmt(s Stmt, sc *scope) (Value, error) {
	switch st := s.(type) {
	case *ExprStmt:
		_, err := in.eval(st.X, sc)
		return nil, err
	case *AssignStmt:
		v, err := in.eval(st.Val, sc)
		if err != nil {
			return nil, err
		}
		if st.Op == ":=" {
			sc.vars[st.Name] = v
		} else {
			if !sc.set(st.Name, v) {
				return nil, fmt.Errorf("assignment to undefined variable %q", st.Name)
			}
		}
		return nil, nil
	case *ReturnStmt:
		if st.Val == nil {
			return returnSignal{val: nil}, nil
		}
		v, err := in.eval(st.Val, sc)
		if err != nil {
			return nil, err
		}
		return returnSignal{val: v}, nil
	case *IfStmt:
		cond, err := in.eval(st.Cond, sc)
		if err != nil {
			return nil, err
		}
		if truthy(cond) {
			return in.execBlock(st.Then, newScope(sc))
		} else if st.Else != nil {
			return in.execBlock(st.Else, newScope(sc))
		}
		return nil, nil
	case *WhileStmt:
		for {
			cond, err := in.eval(st.Cond, sc)
			if err != nil {
				return nil, err
			}
			if !truthy(cond) {
				break
			}
			sig, err := in.execBlock(st.Body, newScope(sc))
			if err != nil {
				return nil, err
			}
			if sig != nil {
				return sig, nil
			}
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown statement %T", s)
}

func (in *Interp) eval(e Expr, sc *scope) (Value, error) {
	switch ex := e.(type) {
	case *IntLit:
		return ex.Val, nil
	case *FloatLit:
		return ex.Val, nil
	case *StringLit:
		return ex.Val, nil
	case *BoolLit:
		return ex.Val, nil
	case *NilLit:
		return nil, nil
	case *Ident:
		if v, ok := sc.get(ex.Name); ok {
			return v, nil
		}
		return nil, fmt.Errorf("undefined variable %q", ex.Name)
	case *Unary:
		return in.evalUnary(ex, sc)
	case *Binary:
		return in.evalBinary(ex, sc)
	case *Call:
		return in.evalCall(ex, sc)
	}
	return nil, fmt.Errorf("unknown expression %T", e)
}

func (in *Interp) evalUnary(ex *Unary, sc *scope) (Value, error) {
	v, err := in.eval(ex.X, sc)
	if err != nil {
		return nil, err
	}
	switch ex.Op {
	case "-":
		switch n := v.(type) {
		case int64:
			return -n, nil
		case float64:
			return -n, nil
		}
		return nil, fmt.Errorf("cannot negate %T", v)
	case "!":
		return !truthy(v), nil
	}
	return nil, fmt.Errorf("unknown unary op %q", ex.Op)
}

func (in *Interp) evalBinary(ex *Binary, sc *scope) (Value, error) {
	// short-circuit logical ops
	if ex.Op == "&&" || ex.Op == "||" {
		l, err := in.eval(ex.L, sc)
		if err != nil {
			return nil, err
		}
		if ex.Op == "&&" && !truthy(l) {
			return false, nil
		}
		if ex.Op == "||" && truthy(l) {
			return true, nil
		}
		r, err := in.eval(ex.R, sc)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	}
	l, err := in.eval(ex.L, sc)
	if err != nil {
		return nil, err
	}
	r, err := in.eval(ex.R, sc)
	if err != nil {
		return nil, err
	}
	return applyBinary(ex.Op, l, r)
}

func applyBinary(op string, l, r Value) (Value, error) {
	// equality works for all types
	switch op {
	case "==":
		return valuesEqual(l, r), nil
	case "!=":
		return !valuesEqual(l, r), nil
	}
	// string concatenation / comparison
	ls, lstr := l.(string)
	rs, rstr := r.(string)
	if lstr && rstr {
		switch op {
		case "+":
			return ls + rs, nil
		case "<":
			return ls < rs, nil
		case "<=":
			return ls <= rs, nil
		case ">":
			return ls > rs, nil
		case ">=":
			return ls >= rs, nil
		}
		return nil, fmt.Errorf("unsupported string op %q", op)
	}
	// numeric: promote to float if either is float
	lf, lok := toFloat(l)
	rf, rok := toFloat(r)
	if !lok || !rok {
		return nil, fmt.Errorf("operator %q needs numbers, got %T and %T", op, l, r)
	}
	li, liok := l.(int64)
	ri, riok := r.(int64)
	bothInt := liok && riok
	switch op {
	case "+":
		if bothInt {
			return li + ri, nil
		}
		return lf + rf, nil
	case "-":
		if bothInt {
			return li - ri, nil
		}
		return lf - rf, nil
	case "*":
		if bothInt {
			return li * ri, nil
		}
		return lf * rf, nil
	case "/":
		if bothInt {
			if ri == 0 {
				return nil, fmt.Errorf("integer division by zero")
			}
			return li / ri, nil
		}
		if rf == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return lf / rf, nil
	case "%":
		if !bothInt {
			return nil, fmt.Errorf("%% requires integers")
		}
		if ri == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return li % ri, nil
	case "<":
		return lf < rf, nil
	case "<=":
		return lf <= rf, nil
	case ">":
		return lf > rf, nil
	case ">=":
		return lf >= rf, nil
	}
	return nil, fmt.Errorf("unknown operator %q", op)
}

func (in *Interp) evalCall(ex *Call, sc *scope) (Value, error) {
	args := make([]Value, len(ex.Args))
	for i, a := range ex.Args {
		v, err := in.eval(a, sc)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	// builtins
	if b, ok := builtins[ex.Callee]; ok {
		return b(in, args)
	}
	fn, ok := in.funcs[ex.Callee]
	if !ok {
		return nil, fmt.Errorf("call to undefined function %q", ex.Callee)
	}
	return in.callFunc(fn, args)
}

// ---- helpers ----

func truthy(v Value) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

func toFloat(v Value) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func valuesEqual(l, r Value) bool {
	// numeric cross-type equality
	if lf, lok := toFloat(l); lok {
		if rf, rok := toFloat(r); rok {
			return lf == rf
		}
	}
	return l == r
}
