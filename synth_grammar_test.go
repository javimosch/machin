package main

import (
	"strings"
	"testing"
)

// TestSynthDiscoversShift: with shifts in the one-parameter grammar, an 8x multiply is
// discovered as `a << 3` (cost 1), not a multiply or a chain of adds.
func TestSynthDiscoversShift(t *testing.T) {
	target, funcs, structs := synthTarget(t, "oct",
		`func oct(a) (r) { r = a * 8 }`,
		`func main() { println(str(oct(2))) }`,
	)
	res := synthesize(target, target.Params, funcs, structs)
	if !res.Found {
		t.Fatalf("expected to discover a cheaper form for a*8; note=%q", res.Note)
	}
	if !strings.Contains(res.Expr, "<<") {
		t.Errorf("expected a shift form, got %q", res.Expr)
	}
	if res.CostAfter != 1 {
		t.Errorf("a << 3 should cost 1, got %d (%q)", res.CostAfter, res.Expr)
	}
}

// TestSynthPrefersCheaperForm: cost-ordered enumeration keeps the CHEAPEST representative per
// behavior, so tripling is the two-add form `a + (a + a)` (cost 2), never the costlier `a * 3`.
func TestSynthPrefersCheaperForm(t *testing.T) {
	target, funcs, structs := synthTarget(t, "tri",
		`func tri(a) (r) { r = a * 3 }`,
		`func main() { println(str(tri(2))) }`,
	)
	res := synthesize(target, target.Params, funcs, structs)
	if !res.Found || res.CostAfter > 2 {
		t.Fatalf("tri should reduce to a cost-2 add form, got %q (cost %d)", res.Expr, res.CostAfter)
	}
	if strings.Contains(res.Expr, "*") {
		t.Errorf("a multiply is costlier than the add form; got %q", res.Expr)
	}
}

// TestSynthGrammar: the one-parameter alphabet carries shifts and the wider constant set; the
// two-parameter alphabet is lean (no shifts) to keep the squared search space tractable.
func TestSynthGrammar(t *testing.T) {
	c1, o1 := synthGrammar(1)
	if !contains(o1, "<<") || !contains(o1, ">>") {
		t.Error("1-param grammar should include shifts")
	}
	if !containsInt(c1, 3) || !containsInt(c1, 4) {
		t.Error("1-param grammar should include the wider constant set")
	}
	c2, o2 := synthGrammar(2)
	if contains(o2, "<<") {
		t.Error("2-param grammar should omit shifts (search tractability)")
	}
	if len(c2) >= len(c1) {
		t.Error("2-param constant set should be leaner than 1-param")
	}
}

// TestSynthEvalIntShifts pins that the fast evaluator computes shifts exactly as the interpreter
// (plain Go int64 semantics), including the arithmetic right shift on a negative operand.
func TestSynthEvalIntShifts(t *testing.T) {
	pidx := map[string]int{"a": 0}
	cases := []struct {
		e    Expr
		env  int64
		want int64
	}{
		{&Binary{Op: "<<", L: &Ident{"a"}, R: &IntLit{3}}, 3, 24},
		{&Binary{Op: ">>", L: &Ident{"a"}, R: &IntLit{1}}, -8, -4}, // arithmetic shift
		{&Binary{Op: "<<", L: &Ident{"a"}, R: &IntLit{1}}, -5, -10},
	}
	for i, c := range cases {
		got, ok := evalInt(c.e, []int64{c.env}, pidx)
		if !ok || got != c.want {
			t.Errorf("case %d: evalInt = (%d,%v), want %d", i, got, ok, c.want)
		}
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func containsInt(xs []int64, v int64) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
