package main

import (
	"fmt"
	"sort"
)

// liftClosures rewrites function literals into top-level "lifted" functions plus
// MakeClosure expressions (closure conversion). A lambda's free variables that
// resolve to an enclosing local become leading parameters of the lifted
// function (NumCaptures of them), captured by value at the MakeClosure site.
func liftClosures(prog *Program) {
	counter := 0
	worklist := make([]*FuncDecl, len(prog.Funcs))
	copy(worklist, prog.Funcs)
	var lifted []*FuncDecl

	for len(worklist) > 0 {
		fn := worklist[0]
		worklist = worklist[1:]
		bound := localNames(fn.Params, fn.Body)
		for _, r := range fn.Returns {
			bound[r] = true // named returns are locals, capturable by lambdas
		}
		if fn.Boxed == nil {
			fn.Boxed = map[string]bool{}
		}
		l := &lifter{
			bound:     bound,
			enclosing: fn,
			counter:   &counter,
			emit: func(nf *FuncDecl) {
				lifted = append(lifted, nf)
				worklist = append(worklist, nf)
			},
		}
		for _, s := range fn.Body {
			l.stmt(s)
		}
	}
	prog.Funcs = append(prog.Funcs, lifted...)
}

type lifter struct {
	bound     map[string]bool
	enclosing *FuncDecl // the function whose body is being lifted
	counter   *int
	emit      func(*FuncDecl)
}

// lift turns a FuncLit into a lifted FuncDecl + a MakeClosure referencing it.
func (l *lifter) lift(lit *FuncLit) Expr {
	free := freeIdents(lit.Body, lit.Params)
	var caps []string
	for name := range free {
		if l.bound[name] {
			caps = append(caps, name)
		}
	}
	sort.Strings(caps)

	// Captures are by reference: each captured variable is heap-boxed in the
	// enclosing function, and the lambda receives the box pointer under the same
	// name. Both sides access it through a dereference, so mutations are shared.
	boxed := map[string]bool{}
	for _, c := range caps {
		l.enclosing.Boxed[c] = true
		boxed[c] = true
	}

	name := fmt.Sprintf("lambda_%d", *l.counter)
	*l.counter++
	fn := &FuncDecl{
		Name:        name,
		Params:      append(append([]string{}, caps...), lit.Params...),
		Body:        lit.Body,
		IsLambda:    true,
		NumCaptures: len(caps),
		Boxed:       boxed,
	}
	l.emit(fn)
	return &MakeClosure{FuncName: name, Captures: caps}
}

func (l *lifter) expr(e Expr) Expr {
	switch ex := e.(type) {
	case *FuncLit:
		return l.lift(ex)
	case *Unary:
		ex.X = l.expr(ex.X)
	case *Binary:
		ex.L = l.expr(ex.L)
		ex.R = l.expr(ex.R)
	case *Call:
		l.exprs(ex.Args)
	case *CallValue:
		ex.Fn = l.expr(ex.Fn)
		l.exprs(ex.Args)
	case *SliceLit:
		l.exprs(ex.Elems)
	case *StructLit:
		l.exprs(ex.Vals)
	case *Index:
		ex.X = l.expr(ex.X)
		ex.Idx = l.expr(ex.Idx)
	case *FieldAccess:
		ex.X = l.expr(ex.X)
	case *Recv:
		ex.Ch = l.expr(ex.Ch)
	}
	return e
}

func (l *lifter) exprs(es []Expr) {
	for i := range es {
		es[i] = l.expr(es[i])
	}
}

func (l *lifter) stmt(s Stmt) {
	switch st := s.(type) {
	case *ExprStmt:
		st.X = l.expr(st.X)
	case *AssignStmt:
		st.Val = l.expr(st.Val)
	case *MultiAssign:
		l.exprs(st.Rhs)
	case *ReturnStmt:
		l.exprs(st.Vals)
	case *IfStmt:
		st.Cond = l.expr(st.Cond)
		l.stmts(st.Then)
		l.stmts(st.Else)
	case *WhileStmt:
		st.Cond = l.expr(st.Cond)
		l.stmts(st.Body)
	case *RangeStmt:
		st.X = l.expr(st.X)
		l.stmts(st.Body)
	case *IndexAssign:
		ix := l.expr(st.Target).(*Index)
		st.Target = ix
		st.Val = l.expr(st.Val)
	case *FieldAssign:
		fa := l.expr(st.Target).(*FieldAccess)
		st.Target = fa
		st.Val = l.expr(st.Val)
	case *SendStmt:
		st.Ch = l.expr(st.Ch)
		st.Val = l.expr(st.Val)
	case *GoStmt:
		l.exprs(st.Call.Args)
	}
}

func (l *lifter) stmts(ss []Stmt) {
	for _, s := range ss {
		l.stmt(s)
	}
}

// localNames returns every name bound in a function: its params plus all names
// declared in its body (flat function scope).
func localNames(params []string, body []Stmt) map[string]bool {
	set := map[string]bool{}
	for _, p := range params {
		set[p] = true
	}
	collectDeclared(body, set)
	return set
}

// collectDeclared adds names introduced by :=, var, range, and multi-:= to set,
// recursing into nested blocks but NOT into nested function literals.
func collectDeclared(body []Stmt, set map[string]bool) {
	for _, s := range body {
		switch st := s.(type) {
		case *AssignStmt:
			if st.Op == ":=" {
				set[st.Name] = true
			}
		case *MultiAssign:
			if st.Op == ":=" {
				for _, n := range st.Names {
					if n != "_" {
						set[n] = true
					}
				}
			}
		case *RangeStmt:
			if st.Key != "" && st.Key != "_" {
				set[st.Key] = true
			}
			if st.Val != "" && st.Val != "_" {
				set[st.Val] = true
			}
			collectDeclared(st.Body, set)
		case *IfStmt:
			collectDeclared(st.Then, set)
			collectDeclared(st.Else, set)
		case *WhileStmt:
			collectDeclared(st.Body, set)
		}
	}
}

// freeIdents returns the identifiers referenced in a lambda body that are not
// bound within it (its params or its own locals), recursing into nested lambdas.
func freeIdents(body []Stmt, params []string) map[string]bool {
	bound := localNames(params, body)
	free := map[string]bool{}
	var we func(e Expr)
	var ws func(s Stmt)
	we = func(e Expr) {
		switch ex := e.(type) {
		case *Ident:
			if !bound[ex.Name] {
				free[ex.Name] = true
			}
		case *FuncLit:
			for n := range freeIdents(ex.Body, ex.Params) {
				if !bound[n] {
					free[n] = true
				}
			}
		case *Unary:
			we(ex.X)
		case *Binary:
			we(ex.L)
			we(ex.R)
		case *Call:
			for _, a := range ex.Args {
				we(a)
			}
		case *CallValue:
			we(ex.Fn)
			for _, a := range ex.Args {
				we(a)
			}
		case *SliceLit:
			for _, a := range ex.Elems {
				we(a)
			}
		case *StructLit:
			for _, a := range ex.Vals {
				we(a)
			}
		case *Index:
			we(ex.X)
			we(ex.Idx)
		case *FieldAccess:
			we(ex.X)
		case *Recv:
			we(ex.Ch)
		}
	}
	ws = func(s Stmt) {
		switch st := s.(type) {
		case *ExprStmt:
			we(st.X)
		case *AssignStmt:
			we(st.Val)
		case *MultiAssign:
			for _, e := range st.Rhs {
				we(e)
			}
		case *ReturnStmt:
			for _, e := range st.Vals {
				we(e)
			}
		case *IfStmt:
			we(st.Cond)
			for _, t := range st.Then {
				ws(t)
			}
			for _, t := range st.Else {
				ws(t)
			}
		case *WhileStmt:
			we(st.Cond)
			for _, t := range st.Body {
				ws(t)
			}
		case *RangeStmt:
			we(st.X)
			for _, t := range st.Body {
				ws(t)
			}
		case *IndexAssign:
			we(st.Target)
			we(st.Val)
		case *FieldAssign:
			we(st.Target)
			we(st.Val)
		case *SendStmt:
			we(st.Ch)
			we(st.Val)
		case *GoStmt:
			for _, a := range st.Call.Args {
				we(a)
			}
		}
	}
	for _, s := range body {
		ws(s)
	}
	return free
}
