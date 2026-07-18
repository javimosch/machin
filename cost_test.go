package main

import "testing"

// TestOpCost checks the relative latency weights.
func TestOpCost(t *testing.T) {
	if opCost("*") != 4 || opCost("/") != 5 || opCost("%") != 5 {
		t.Error("mul/div/mod weights")
	}
	if opCost("<<") != 1 || opCost("+") != 1 || opCost("==") != 1 || opCost("&&") != 1 {
		t.Error("cheap-op weights")
	}
}

// TestExprCost sums operation weights over an expression tree.
func TestExprCost(t *testing.T) {
	cases := []struct {
		name string
		e    Expr
		want int
	}{
		{"literal", &IntLit{5}, 0},
		{"ident", &Ident{"a"}, 0},
		{"mul", &Binary{Op: "*", L: &Ident{"a"}, R: &IntLit{8}}, 4},
		{"shift", &Binary{Op: "<<", L: &Ident{"a"}, R: &IntLit{3}}, 1},
		{"a + b*c", &Binary{Op: "+", L: &Ident{"a"}, R: &Binary{Op: "*", L: &Ident{"b"}, R: &Ident{"c"}}}, 5},
		{"unary", &Unary{Op: "-", X: &Ident{"a"}}, 1},
		{"call", &Call{Callee: "f", Args: []Expr{&Binary{Op: "*", L: &Ident{"a"}, R: &IntLit{2}}}}, 5 + 4},
		{"index", &Index{X: &Ident{"xs"}, Idx: &Ident{"i"}}, 2},
		{"field", &FieldAccess{X: &Ident{"p"}, Name: "x"}, 1},
		{"slicelit", &SliceLit{Elem: "int", Elems: []Expr{&Binary{Op: "*", L: &Ident{"a"}, R: &IntLit{2}}}}, 4},
		{"structlit", &StructLit{Type: "P", Vals: []Expr{&Binary{Op: "+", L: &Ident{"a"}, R: &Ident{"b"}}}}, 1},
		{"recv", &Recv{Ch: &Ident{"ch"}}, 5},
		{"callvalue", &CallValue{Fn: &Ident{"g"}, Args: []Expr{&Ident{"a"}}}, 5},
	}
	for _, c := range cases {
		if got := exprCost(c.e); got != c.want {
			t.Errorf("%s: exprCost = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestStmtCostLoopWeighting: a multiply in a loop body counts loopFactor× a straight-line one.
func TestStmtCostLoopWeighting(t *testing.T) {
	mul := &AssignStmt{Name: "a", Op: "=", Val: &Binary{Op: "*", L: &Ident{"a"}, R: &IntLit{4}}}
	straight := stmtCost(mul) // 4
	loop := stmtCost(&WhileStmt{Cond: &BoolLit{true}, Body: []Stmt{mul}})
	if straight != 4 {
		t.Fatalf("straight-line mul cost = %d, want 4", straight)
	}
	if loop != loopFactor*4 {
		t.Errorf("loop-body mul cost = %d, want %d", loop, loopFactor*4)
	}
	// range loop is weighted too
	rng := stmtCost(&RangeStmt{Key: "i", X: &Ident{"xs"}, Body: []Stmt{mul}})
	if rng != loopFactor*4 {
		t.Errorf("range-body mul cost = %d, want %d", rng, loopFactor*4)
	}
}

// TestStmtCostKinds exercises the remaining statement arms.
func TestStmtCostKinds(t *testing.T) {
	mul := &Binary{Op: "*", L: &Ident{"a"}, R: &IntLit{2}} // cost 4
	cases := []struct {
		s    Stmt
		want int
	}{
		{&ExprStmt{X: mul}, 4},
		{&ReturnStmt{Vals: []Expr{mul}}, 4},
		{&MultiAssign{Rhs: []Expr{mul, mul}}, 8},
		{&IfStmt{Cond: &Binary{Op: "<", L: &Ident{"a"}, R: &Ident{"b"}}, Then: []Stmt{&ExprStmt{X: mul}}}, 1 + 4},
		{&IndexAssign{Target: &Index{X: &Ident{"xs"}, Idx: &Ident{"i"}}, Val: mul}, 2 + 4},
		{&FieldAssign{Target: &FieldAccess{X: &Ident{"p"}, Name: "x"}, Val: mul}, 1 + 4},
		{&SendStmt{Ch: &Ident{"c"}, Val: mul}, 5 + 4},
		{&ArenaStmt{Body: []Stmt{&ExprStmt{X: mul}}}, 4},
		{&BreakStmt{}, 0},
	}
	for i, c := range cases {
		if got := stmtCost(c.s); got != c.want {
			t.Errorf("case %d: stmtCost = %d, want %d", i, got, c.want)
		}
	}
}

// TestCostPct covers the percentage helper including the zero-before guard.
func TestCostPct(t *testing.T) {
	if costPct(4, 1) != 75 {
		t.Errorf("costPct(4,1) = %d, want 75", costPct(4, 1))
	}
	if costPct(0, 0) != 0 {
		t.Errorf("costPct(0,0) = %d, want 0", costPct(0, 0))
	}
}

// TestOptimizeReportsCost: a proven rewrite carries a before>after static cost, and the totals
// aggregate across functions.
func TestOptimizeReportsCost(t *testing.T) {
	rep := optProg(t,
		`func scale(x) (r) { r = x * 8 }`,
		`func main() { println(str(scale(2))) }`,
	)
	f, _ := funcResult(rep, "scale")
	if f.CostBefore != 4 || f.CostAfter != 1 {
		t.Errorf("scale cost %d→%d, want 4→1", f.CostBefore, f.CostAfter)
	}
	if rep.CostBefore != 4 || rep.CostAfter != 1 {
		t.Errorf("report totals %d→%d, want 4→1", rep.CostBefore, rep.CostAfter)
	}
	// a rejected-only function contributes no cost delta
	rep2 := optProg(t,
		`func halve(x) (r) { r = x / 2 }`,
		`func main() { println(str(halve(9))) }`,
	)
	if rep2.CostBefore != 0 || rep2.CostAfter != 0 {
		t.Errorf("rejected-only totals %d→%d, want 0→0", rep2.CostBefore, rep2.CostAfter)
	}
}
