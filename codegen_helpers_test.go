package main

import "testing"

func TestCTypeName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"int", "int64_t"},
		{"float", "double"},
		{"bool", "int"},
		{"string", "char*"},
		{"func", "mfl_closure"},
		{"[]int", "mfl_slice"},
		{"map[string]int", "mfl_map*"},
		{"chan int", "mfl_chan*"},
		{"Point", "mfl_Point"},
	}
	for _, tt := range tests {
		if got := cTypeName(tt.in); got != tt.want {
			t.Errorf("cTypeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCZero(t *testing.T) {
	tests := []struct {
		k    Kind
		want string
	}{
		{KInt, "0"},
		{KBool, "0"},
		{KFloat, "0.0"},
		{KString, "\"\""},
		{KSlice, "{0}"},
		{KStruct, "{0}"},
		{KFunc, "{0}"},
		{KBytes, "{0}"},
	}
	for _, tt := range tests {
		if got := cZero(tt.k); got != tt.want {
			t.Errorf("cZero(%v) = %q, want %q", tt.k, got, tt.want)
		}
	}
}

func TestPrependEach(t *testing.T) {
	if got := prependEach(", ", nil); got != "" {
		t.Errorf("prependEach(sep, nil) = %q, want \"\"", got)
	}
	if got := prependEach(", ", []string{}); got != "" {
		t.Errorf("prependEach(sep, empty) = %q, want \"\"", got)
	}
	if got := prependEach(", ", []string{"a"}); got != ", a" {
		t.Errorf("prependEach(sep, [a]) = %q, want \", a\"", got)
	}
	if got := prependEach(", ", []string{"a", "b"}); got != ", a, b" {
		t.Errorf("prependEach(sep, [a,b]) = %q, want \", a, b\"", got)
	}
}

func TestExprHasSideEffect(t *testing.T) {
	tests := []struct {
		name string
		e    Expr
		want bool
	}{
		{"call", &Call{Callee: "f"}, true},
		{"recv", &Recv{Ch: &Ident{Name: "ch"}}, true},
		{"plain ident", &Ident{Name: "x"}, false},
		{"unary of pure", &Unary{Op: "-", X: &Ident{Name: "x"}}, false},
		{"unary of call", &Unary{Op: "-", X: &Call{Callee: "f"}}, true},
		{"binary of pure operands", &Binary{Op: "+", L: &Ident{Name: "x"}, R: &Ident{Name: "y"}}, false},
		{"binary with impure operand", &Binary{Op: "+", L: &Ident{Name: "x"}, R: &Call{Callee: "f"}}, true},
		{"index pure", &Index{X: &Ident{Name: "a"}, Idx: &Ident{Name: "i"}}, false},
		{"index with impure idx", &Index{X: &Ident{Name: "a"}, Idx: &Call{Callee: "f"}}, true},
		{"field access pure", &FieldAccess{X: &Ident{Name: "p"}, Name: "x"}, false},
		{"field access impure", &FieldAccess{X: &Call{Callee: "f"}, Name: "x"}, true},
		{"slice lit pure", &SliceLit{Elems: []Expr{&Ident{Name: "a"}, &Ident{Name: "b"}}}, false},
		{"slice lit impure element", &SliceLit{Elems: []Expr{&Ident{Name: "a"}, &Call{Callee: "f"}}}, true},
		{"struct lit pure", &StructLit{Type: "P", Vals: []Expr{&Ident{Name: "a"}}}, false},
		{"struct lit impure", &StructLit{Type: "P", Vals: []Expr{&Call{Callee: "f"}}}, true},
	}
	for _, tt := range tests {
		if got := exprHasSideEffect(tt.e); got != tt.want {
			t.Errorf("%s: exprHasSideEffect() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
