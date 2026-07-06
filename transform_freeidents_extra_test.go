package main

import "testing"

// These cover freeIdents expression/statement cases not exercised by
// TestFreeIdents* in transform_test.go: Unary, CallValue, SliceLit,
// FieldAccess expressions, and RangeStmt, FieldAssign, GoStmt, ArenaStmt
// statements.

func TestFreeIdentsUnaryExpr(t *testing.T) {
	body := []Stmt{
		&ReturnStmt{Vals: []Expr{&Unary{Op: "-", X: &Ident{Name: "outer"}}}},
	}
	free := freeIdents(body, []string{})
	if !free["outer"] {
		t.Errorf("expected Unary operand to be free: %v", free)
	}
}

func TestFreeIdentsCallValueExpr(t *testing.T) {
	body := []Stmt{
		&ExprStmt{X: &CallValue{Fn: &Ident{Name: "fn"}, Args: []Expr{&Ident{Name: "arg"}}}},
	}
	free := freeIdents(body, []string{})
	if !free["fn"] || !free["arg"] {
		t.Errorf("expected CallValue's Fn and Args to be free: %v", free)
	}
}

func TestFreeIdentsSliceLitExpr(t *testing.T) {
	body := []Stmt{
		&ReturnStmt{Vals: []Expr{&SliceLit{Elem: "int", Elems: []Expr{&Ident{Name: "elem"}}}}},
	}
	free := freeIdents(body, []string{})
	if !free["elem"] {
		t.Errorf("expected SliceLit element to be free: %v", free)
	}
}

func TestFreeIdentsFieldAccessExpr(t *testing.T) {
	body := []Stmt{
		&ReturnStmt{Vals: []Expr{&FieldAccess{X: &Ident{Name: "obj"}, Name: "field"}}},
	}
	free := freeIdents(body, []string{})
	if !free["obj"] {
		t.Errorf("expected FieldAccess base to be free: %v", free)
	}
}

func TestFreeIdentsRangeStmt(t *testing.T) {
	body := []Stmt{
		&RangeStmt{Key: "i", Val: "v", X: &Ident{Name: "xs"}, Body: []Stmt{
			&ExprStmt{X: &Ident{Name: "captured"}},
		}},
	}
	free := freeIdents(body, []string{})
	if !free["xs"] {
		t.Errorf("expected RangeStmt's iterated expr to be free: %v", free)
	}
	if !free["captured"] {
		t.Errorf("expected RangeStmt body reference to be free: %v", free)
	}
}

func TestFreeIdentsFieldAssignStmt(t *testing.T) {
	body := []Stmt{
		&FieldAssign{
			Target: &FieldAccess{X: &Ident{Name: "obj"}, Name: "field"},
			Val:    &Ident{Name: "val"},
		},
	}
	free := freeIdents(body, []string{})
	if !free["obj"] || !free["val"] {
		t.Errorf("expected FieldAssign target and value to be free: %v", free)
	}
}

func TestFreeIdentsGoStmt(t *testing.T) {
	body := []Stmt{
		&GoStmt{Call: &Call{Callee: "worker", Args: []Expr{&Ident{Name: "arg"}}}},
	}
	free := freeIdents(body, []string{})
	if !free["arg"] {
		t.Errorf("expected GoStmt call argument to be free: %v", free)
	}
}

func TestFreeIdentsArenaStmt(t *testing.T) {
	body := []Stmt{
		&ArenaStmt{Body: []Stmt{
			&ExprStmt{X: &Ident{Name: "captured"}},
		}},
	}
	free := freeIdents(body, []string{})
	if !free["captured"] {
		t.Errorf("expected ArenaStmt body reference to be free: %v", free)
	}
}
