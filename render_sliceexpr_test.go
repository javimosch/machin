package main

import (
	"strings"
	"testing"
)

// render.go's expr renderer had no *SliceExpr case, so any function body
// containing a slice-range expression (x[lo:hi], x[lo:], x[:hi]) fell to the
// default "unsupported node" branch: renderFuncSrc set complete=false and
// emitted "/*?*/" instead of the slice expression. That silently breaks the
// `machin optimize` command's rendered output for any program using #557
// slice-range syntax.
func TestRenderSliceExpr(t *testing.T) {
	cases := []struct {
		expr Expr
		want string
	}{
		{&SliceExpr{X: &Ident{"xs"}, Lo: &IntLit{1}, Hi: &IntLit{3}}, "xs[1:3]"},
		{&SliceExpr{X: &Ident{"xs"}, Lo: &IntLit{1}}, "xs[1:]"},
		{&SliceExpr{X: &Ident{"xs"}, Hi: &IntLit{3}}, "xs[:3]"},
	}
	for _, c := range cases {
		f := &FuncDecl{Name: "f", Params: []string{"xs"}, Body: []Stmt{
			&ExprStmt{X: c.expr},
		}}
		src, complete := renderFuncSrc(f)
		if !complete {
			t.Fatalf("renderFuncSrc incomplete for %v: %s", c.expr, src)
		}
		if want := c.want; !strings.Contains(src, want) {
			t.Fatalf("renderFuncSrc(%v) = %q, want to contain %q", c.expr, src, want)
		}
	}
}
