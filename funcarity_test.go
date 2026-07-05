package main

import "testing"

func TestReturnArity(t *testing.T) {
	cases := []struct {
		name string
		body []Stmt
		want int
	}{
		{"empty body", nil, 0},
		{"bare return", []Stmt{&ReturnStmt{}}, 0},
		{"direct return with values", []Stmt{&ReturnStmt{Vals: []Expr{nil, nil}}}, 2},
		{
			"nested in if-then",
			[]Stmt{&IfStmt{Then: []Stmt{&ReturnStmt{Vals: []Expr{nil}}}}},
			1,
		},
		{
			"nested in if-else",
			[]Stmt{&IfStmt{Else: []Stmt{&ReturnStmt{Vals: []Expr{nil, nil, nil}}}}},
			3,
		},
		{
			"nested in while",
			[]Stmt{&WhileStmt{Body: []Stmt{&ReturnStmt{Vals: []Expr{nil}}}}},
			1,
		},
		{
			"nested in range",
			[]Stmt{&RangeStmt{Body: []Stmt{&ReturnStmt{Vals: []Expr{nil, nil}}}}},
			2,
		},
		{
			"nested in arena",
			[]Stmt{&ArenaStmt{Body: []Stmt{&ReturnStmt{Vals: []Expr{nil}}}}},
			1,
		},
		{
			"returns in both then and else with different arities",
			[]Stmt{&IfStmt{
				Then: []Stmt{&ReturnStmt{Vals: []Expr{nil}}},
				Else: []Stmt{&ReturnStmt{Vals: []Expr{nil, nil}}},
			}},
			1, // returns arity of then (first non-zero found)
		},
	}
	for _, c := range cases {
		if got := returnArity(c.body); got != c.want {
			t.Errorf("%s: returnArity() = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestFuncArity(t *testing.T) {
	cases := []struct {
		name string
		fn   *FuncDecl
		want int
	}{
		{"named returns win", &FuncDecl{Returns: []string{"a", "b"}}, 2},
		{
			"falls back to inferred body arity",
			&FuncDecl{Body: []Stmt{&ReturnStmt{Vals: []Expr{nil, nil, nil}}}},
			3,
		},
		{"no returns at all", &FuncDecl{}, 0},
	}
	for _, c := range cases {
		if got := funcArity(c.fn); got != c.want {
			t.Errorf("%s: funcArity() = %d, want %d", c.name, got, c.want)
		}
	}
}
