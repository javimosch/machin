package main

import "testing"

// Covers sexprExpr/sexprStmt/sexprFunc/sexprGlobal/sexprExtern, none of which had
// direct unit coverage: parsetest_test.go only exercised sexprFields/sexprType via
// the higher-level ParseType path.

func TestSexprExprAllKinds(t *testing.T) {
	cases := []struct {
		name string
		e    Expr
		want string
	}{
		{"int", &IntLit{Val: 42}, "(int 42)"},
		{"float", &FloatLit{Val: 1.5}, "(float 1.5)"},
		{"string", &StringLit{Val: "hi"}, "(str 6869)"},
		{"bool true", &BoolLit{Val: true}, "(bool true)"},
		{"bool false", &BoolLit{Val: false}, "(bool false)"},
		{"nil", &NilLit{}, "(nil)"},
		{"ident", &Ident{Name: "x"}, "(id x)"},
		{"unary", &Unary{Op: "-", X: &IntLit{Val: 1}}, "(unary - (int 1))"},
		{"binary", &Binary{Op: "+", L: &IntLit{Val: 1}, R: &IntLit{Val: 2}}, "(bin + (int 1) (int 2))"},
		{"call", &Call{Callee: "f", Args: []Expr{&IntLit{Val: 1}}}, "(call f (int 1))"},
		{"call spread", &Call{Callee: "f", Args: []Expr{&Ident{Name: "xs"}}, Spread: true}, "(call f (id xs) ...)"},
		{"callvalue", &CallValue{Fn: &Ident{Name: "g"}, Args: []Expr{&IntLit{Val: 3}}}, "(callv (id g) (int 3))"},
		{"index", &Index{X: &Ident{Name: "a"}, Idx: &IntLit{Val: 0}}, "(index (id a) (int 0))"},
		{"field", &FieldAccess{X: &Ident{Name: "p"}, Name: "x"}, "(field (id p) x)"},
		{"slice", &SliceLit{Elem: "int", Elems: []Expr{&IntLit{Val: 1}, &IntLit{Val: 2}}}, "(slice int (int 1) (int 2))"},
		{"struct positional", &StructLit{Type: "Point", Vals: []Expr{&IntLit{Val: 1}, &IntLit{Val: 2}}}, "(struct Point (keys) (int 1) (int 2))"},
		{"struct named", &StructLit{Type: "Point", FieldNames: []string{"x", "y"}, Vals: []Expr{&IntLit{Val: 1}, &IntLit{Val: 2}}}, "(struct Point (keys x y) (int 1) (int 2))"},
		{"makechan", &MakeChan{Elem: "int"}, "(makechan int)"},
		{"makemap", &MakeMap{Key: "string", Val: "int"}, "(makemap string int)"},
		{"recv", &Recv{Ch: &Ident{Name: "ch"}}, "(recv (id ch))"},
		{"funclit", &FuncLit{Params: []string{"a"}, Body: []Stmt{&BreakStmt{}}}, "(funclit (params a) (body (break)))"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sexprExpr(c.e); got != c.want {
				t.Errorf("sexprExpr(%s) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestSexprExprUnhandled(t *testing.T) {
	if got := sexprExpr(&MakeClosure{FuncName: "f"}); got != "(?)" {
		t.Errorf("sexprExpr(unhandled) = %q, want (?)", got)
	}
}

func TestSexprStmtAllKinds(t *testing.T) {
	cases := []struct {
		name string
		s    Stmt
		want string
	}{
		{"expr", &ExprStmt{X: &IntLit{Val: 1}}, "(expr (int 1))"},
		{"assign", &AssignStmt{Name: "x", Op: ":=", Val: &IntLit{Val: 1}}, "(assign := x (int 1))"},
		{"multi", &MultiAssign{Names: []string{"a", "b"}, Op: ":=", Rhs: []Expr{&IntLit{Val: 1}, &IntLit{Val: 2}}}, "(multi := (names a b) (int 1) (int 2))"},
		{"return", &ReturnStmt{Vals: []Expr{&IntLit{Val: 1}}}, "(return (int 1))"},
		{"break", &BreakStmt{}, "(break)"},
		{"continue", &ContinueStmt{}, "(continue)"},
		{"if", &IfStmt{Cond: &BoolLit{Val: true}, Then: []Stmt{&BreakStmt{}}, Else: []Stmt{&ContinueStmt{}}}, "(if (bool true) (then (break)) (else (continue)))"},
		{"while", &WhileStmt{Cond: &BoolLit{Val: true}, Body: []Stmt{&BreakStmt{}}}, "(while (bool true) (body (break)))"},
		{"range", &RangeStmt{Key: "i", Val: "v", X: &Ident{Name: "xs"}, Body: []Stmt{&BreakStmt{}}}, "(range i v (id xs) (body (break)))"},
		{"idxassign", &IndexAssign{Target: &Index{X: &Ident{Name: "a"}, Idx: &IntLit{Val: 0}}, Val: &IntLit{Val: 1}}, "(idxassign (index (id a) (int 0)) (int 1))"},
		{"fldassign", &FieldAssign{Target: &FieldAccess{X: &Ident{Name: "p"}, Name: "x"}, Val: &IntLit{Val: 1}}, "(fldassign (field (id p) x) (int 1))"},
		{"send", &SendStmt{Ch: &Ident{Name: "ch"}, Val: &IntLit{Val: 1}}, "(send (id ch) (int 1))"},
		{"go", &GoStmt{Call: &Call{Callee: "f"}}, "(go (call f))"},
		{"arena", &ArenaStmt{Body: []Stmt{&BreakStmt{}}}, "(arena (body (break)))"},
		{
			"select recv+send+default",
			&SelectStmt{
				Cases: []SelectCase{
					{RecvCh: &Ident{Name: "ch1"}, Name: "v", OkName: "ok", Body: []Stmt{&BreakStmt{}}},
					{SendCh: &Ident{Name: "ch2"}, SendVal: &IntLit{Val: 1}, Body: []Stmt{&ContinueStmt{}}},
				},
				HasDefault: true,
				Default:    []Stmt{&BreakStmt{}},
			},
			"(select (case recv v ok (id ch1) (body (break))) (case send (id ch2) (int 1) (body (continue))) (default (body (break))))",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sexprStmt(c.s); got != c.want {
				t.Errorf("sexprStmt(%s) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestSexprStmtUnhandled(t *testing.T) {
	if got := sexprStmt(nil); got != "(?s)" {
		t.Errorf("sexprStmt(unhandled) = %q, want (?s)", got)
	}
}

func TestSexprFunc(t *testing.T) {
	fn := &FuncDecl{
		Name:     "add",
		Params:   []string{"a", "b"},
		Variadic: true,
		Exported: true,
		Returns:  []string{"int"},
		Body:     []Stmt{&ReturnStmt{Vals: []Expr{&Ident{Name: "a"}}}},
	}
	want := "(func add export (params a b...) (returns int) (body (return (id a))))"
	if got := sexprFunc(fn); got != want {
		t.Errorf("sexprFunc = %q, want %q", got, want)
	}
}

func TestSexprGlobal(t *testing.T) {
	gv := &GlobalVar{Name: "counter", Init: &IntLit{Val: 0}}
	want := "(global counter (int 0))"
	if got := sexprGlobal(gv); got != want {
		t.Errorf("sexprGlobal = %q, want %q", got, want)
	}
}

func TestSexprExtern(t *testing.T) {
	ed := &ExternDecl{
		Lib:    "m",
		Header: "math.h",
		Links:  []string{"m"},
		CFlags: "-O2",
		Structs: []ExternStruct{
			{Name: "Color", Fields: []ExternField{{Name: "r", CType: "u8"}}},
		},
		Funcs: []ExternFunc{
			{Name: "sqrt", Params: []string{"float"}, Ret: "float"},
		},
	}
	want := "(extern m (header math.h) (links m) (cflags -O2) (cstructs (cstruct Color (fields (f r u8)))) (fns (fn sqrt (params float) float)))"
	if got := sexprExtern(ed); got != want {
		t.Errorf("sexprExtern = %q, want %q", got, want)
	}
}
