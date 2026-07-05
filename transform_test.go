package main

import "testing"

func TestLocalNames(t *testing.T) {
	body := []Stmt{
		&AssignStmt{Name: "x", Op: ":=", Val: &Ident{Name: "y"}},
		&MultiAssign{Names: []string{"a", "_", "b"}, Op: ":=", Rhs: []Expr{&Ident{Name: "z"}}},
		&IfStmt{
			Cond: &Ident{Name: "cond"},
			Then: []Stmt{&AssignStmt{Name: "inner", Op: ":=", Val: &Ident{Name: "w"}}},
		},
	}
	got := localNames([]string{"p", "q"}, body)
	want := []string{"p", "q", "x", "a", "b", "inner"}
	for _, n := range want {
		if !got[n] {
			t.Errorf("localNames missing %q: %v", n, got)
		}
	}
	if got["_"] {
		t.Errorf("localNames must not bind the blank identifier")
	}
	if got["z"] {
		t.Errorf("localNames must not bind rhs-only identifiers")
	}
}

func TestLiftClosuresCapturesEnclosingLocal(t *testing.T) {
	// func main() { x := 1; f := func() int { return x }; return f }
	main := &FuncDecl{
		Name: "main",
		Body: []Stmt{
			&AssignStmt{Name: "x", Op: ":=", Val: &IntLit{Val: 1}},
			&AssignStmt{Name: "f", Op: ":=", Val: &FuncLit{
				Body: []Stmt{&ReturnStmt{Vals: []Expr{&Ident{Name: "x"}}}},
			}},
			&ReturnStmt{Vals: []Expr{&Ident{Name: "f"}}},
		},
	}
	prog := &Program{Funcs: []*FuncDecl{main}}

	liftClosures(prog)

	if len(prog.Funcs) != 2 {
		t.Fatalf("want main + 1 lifted func, got %d funcs", len(prog.Funcs))
	}
	lifted := prog.Funcs[1]
	if !lifted.IsLambda || lifted.NumCaptures != 1 {
		t.Fatalf("lifted func: IsLambda=%v NumCaptures=%d, want true/1", lifted.IsLambda, lifted.NumCaptures)
	}
	if len(lifted.Params) != 1 || lifted.Params[0] != "x" {
		t.Fatalf("lifted func params = %v, want [x] (capture as leading param)", lifted.Params)
	}
	if !lifted.Boxed["x"] {
		t.Errorf("lifted func must mark captured param %q boxed", "x")
	}
	if !main.Boxed["x"] {
		t.Errorf("enclosing func must box captured local %q so it's shared by reference", "x")
	}

	assign, ok := main.Body[1].(*AssignStmt)
	if !ok {
		t.Fatalf("main.Body[1] = %T, want *AssignStmt", main.Body[1])
	}
	mc, ok := assign.Val.(*MakeClosure)
	if !ok {
		t.Fatalf("f's initializer = %T, want *MakeClosure (FuncLit must be replaced in place)", assign.Val)
	}
	if mc.FuncName != lifted.Name {
		t.Errorf("MakeClosure.FuncName = %q, want %q", mc.FuncName, lifted.Name)
	}
	if len(mc.Captures) != 1 || mc.Captures[0] != "x" {
		t.Errorf("MakeClosure.Captures = %v, want [x]", mc.Captures)
	}
}

func TestCollectDeclaredNestedFuncLitNotRecursed(t *testing.T) {
	// collectDeclared must not descend into a nested FuncLit's own body — it has
	// no case for *FuncLit, so a := inside it must not leak into the outer set.
	body := []Stmt{
		&AssignStmt{Name: "outer", Op: ":=", Val: &FuncLit{
			Params: []string{"n"},
			Body:   []Stmt{&AssignStmt{Name: "leaked", Op: ":=", Val: &Ident{Name: "n"}}},
		}},
	}
	set := map[string]bool{}
	collectDeclared(body, set)
	if !set["outer"] {
		t.Errorf("expected outer to be declared: %v", set)
	}
	if set["leaked"] {
		t.Errorf("collectDeclared must not recurse into nested FuncLit bodies: %v", set)
	}
}

func TestCollectDeclaredRangeKeyValAndPlainWhile(t *testing.T) {
	body := []Stmt{
		&RangeStmt{Key: "i", Val: "_", X: &Ident{Name: "xs"}, Body: []Stmt{
			&AssignStmt{Name: "sq", Op: ":=", Val: &Ident{Name: "i"}},
		}},
		&WhileStmt{Cond: &Ident{Name: "cond"}, Body: []Stmt{
			&AssignStmt{Name: "acc", Op: ":=", Val: &Ident{Name: "i"}},
		}},
	}
	set := map[string]bool{}
	collectDeclared(body, set)
	for _, n := range []string{"i", "sq", "acc"} {
		if !set[n] {
			t.Errorf("expected %q declared: %v", n, set)
		}
	}
	if set["_"] {
		t.Errorf("collectDeclared must not bind the blank range value")
	}
}

func TestFreeIdentsCapturesOuterOnly(t *testing.T) {
	// A lambda referencing its own param, its own local, and an outer name: only
	// the outer name should come back free.
	body := []Stmt{
		&AssignStmt{Name: "local", Op: ":=", Val: &Ident{Name: "n"}},
		&ReturnStmt{Vals: []Expr{
			&Binary{Op: "+", L: &Ident{Name: "local"}, R: &Ident{Name: "outer"}},
		}},
	}
	free := freeIdents(body, []string{"n"})
	if free["n"] || free["local"] {
		t.Errorf("freeIdents must not free bound params/locals: %v", free)
	}
	if !free["outer"] {
		t.Errorf("freeIdents must free references to outer names: %v", free)
	}
}

func TestFreeIdentsCallCalleeCapturedWhenNotBound(t *testing.T) {
	// A Call's Callee is treated as a potential captured closure variable unless
	// it is bound locally — this lets `fn()` capture a param/local named fn.
	body := []Stmt{
		&ExprStmt{X: &Call{Callee: "fn", Args: []Expr{&Ident{Name: "n"}}}},
	}
	free := freeIdents(body, []string{"n"})
	if !free["fn"] {
		t.Errorf("expected unbound call callee %q to be free: %v", "fn", free)
	}
	if free["n"] {
		t.Errorf("bound param must not be free: %v", free)
	}
}

func TestFreeIdentsNestedFuncLitRecursion(t *testing.T) {
	// A free identifier inside a nested lambda that isn't bound by either lambda
	// must propagate up as free.
	inner := &FuncLit{Params: []string{}, Body: []Stmt{
		&ReturnStmt{Vals: []Expr{&Ident{Name: "captured"}}},
	}}
	body := []Stmt{&ReturnStmt{Vals: []Expr{inner}}}
	free := freeIdents(body, []string{})
	if !free["captured"] {
		t.Errorf("expected nested lambda's free identifier to propagate up: %v", free)
	}
}

func TestLiftClosuresMultipleCapturesAndNesting(t *testing.T) {
	// func main() { x := 1; y := 2; f := func() int { if x > 0 { return y } else { return 0 } } }
	main := &FuncDecl{
		Name: "main",
		Body: []Stmt{
			&AssignStmt{Name: "x", Op: ":=", Val: &IntLit{Val: 1}},
			&AssignStmt{Name: "y", Op: ":=", Val: &IntLit{Val: 2}},
			&AssignStmt{Name: "f", Op: ":=", Val: &FuncLit{
				Body: []Stmt{&IfStmt{
					Cond: &Binary{Op: ">", L: &Ident{Name: "x"}, R: &IntLit{Val: 0}},
					Then: []Stmt{&ReturnStmt{Vals: []Expr{&Ident{Name: "y"}}}},
					Else: []Stmt{&ReturnStmt{Vals: []Expr{&IntLit{Val: 0}}}},
				}},
			}},
		},
	}
	prog := &Program{Funcs: []*FuncDecl{main}}

	liftClosures(prog)

	if len(prog.Funcs) != 2 {
		t.Fatalf("want main + 1 lifted func, got %d funcs", len(prog.Funcs))
	}
	lifted := prog.Funcs[1]
	if lifted.NumCaptures != 2 {
		t.Fatalf("lifted func NumCaptures = %d, want 2 (x, y)", lifted.NumCaptures)
	}
	if len(lifted.Params) < 2 || lifted.Params[0] != "x" || lifted.Params[1] != "y" {
		t.Fatalf("lifted func params = %v, want [x, y, ...] (captures sorted first)", lifted.Params)
	}
}
