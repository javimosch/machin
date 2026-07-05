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
