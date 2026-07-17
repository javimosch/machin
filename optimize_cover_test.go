package main

import "testing"

// TestOptimizeTransformAndRender drives transformStmt/transformExpr and the renderer across
// every node kind they handle: the identity transform must clone the tree, and the renderer
// must reproduce source that reparses. This covers the branches arithmetic-only fixtures miss.
func TestOptimizeTransformAndRender(t *testing.T) {
	id := func(e Expr) Expr { return e }
	body := []Stmt{
		&ExprStmt{X: &Call{Callee: "f", Args: []Expr{&Ident{"a"}, &IntLit{2}}}},
		&AssignStmt{Name: "x", Op: ":=", Val: &Unary{Op: "-", X: &Ident{"a"}}},
		&MultiAssign{Names: []string{"p", "q"}, Op: ":=", Rhs: []Expr{&IntLit{1}, &IntLit{2}}},
		&IfStmt{
			Cond: &Binary{Op: "<", L: &Ident{"a"}, R: &Binary{Op: "*", L: &Ident{"b"}, R: &IntLit{2}}},
			Then: []Stmt{&ReturnStmt{Vals: []Expr{&Ident{"a"}, &Ident{"b"}}}},
			Else: []Stmt{&BreakStmt{}, &ContinueStmt{}},
		},
		&WhileStmt{Cond: &BoolLit{true}, Body: []Stmt{
			&IndexAssign{Target: &Index{X: &Ident{"xs"}, Idx: &IntLit{0}}, Val: &IntLit{9}},
			&FieldAssign{Target: &FieldAccess{X: &Ident{"p"}, Name: "x"}, Val: &FloatLit{1.5}},
		}},
		&RangeStmt{Key: "i", Val: "v", X: &Ident{"xs"}, Body: []Stmt{
			&ExprStmt{X: &SliceLit{Elem: "int", Elems: []Expr{&IntLit{1}, &IntLit{2}}}},
			&ExprStmt{X: &StructLit{Type: "P", FieldNames: []string{"x", "y"}, Vals: []Expr{&IntLit{1}, &IntLit{2}}}},
			&ExprStmt{X: &StructLit{Type: "P", Vals: []Expr{&IntLit{3}, &IntLit{4}}}},
			&ExprStmt{X: &CallValue{Fn: &Ident{"g"}, Args: []Expr{&IntLit{5}}}},
			&ExprStmt{X: &FieldAccess{X: &Index{X: &Ident{"xs"}, Idx: &Ident{"i"}}, Name: "z"}},
			&ExprStmt{X: &StringLit{Val: "hi"}},
			&ExprStmt{X: &NilLit{}},
		}},
		&ArenaStmt{Body: []Stmt{&SendStmt{Ch: &Ident{"ch"}, Val: &IntLit{1}}}},
	}
	// identity transform preserves the rendered form
	before := renderStmtsSrc(body)
	after := renderStmtsSrc(transformStmts(body, id))
	if before != after {
		t.Fatalf("identity transform changed rendering:\n before %q\n after  %q", before, after)
	}
	// full function renders completely
	f := &FuncDecl{Name: "big", Params: []string{"a", "b", "xs", "ch"}, Returns: []string{"r"}, Body: body}
	src, complete := renderFuncSrc(f)
	if !complete {
		t.Fatalf("renderer incomplete for a supported body: %s", src)
	}
	// send-stmt is not a top-level arithmetic target but transformStmt must still clone it
	transformStmts([]Stmt{&SendStmt{Ch: &Ident{"c"}, Val: &Binary{Op: "+", L: &IntLit{1}, R: &IntLit{2}}}}, id)
	// nullary-return + bare return render branches
	renderStmtsSrc([]Stmt{&ReturnStmt{}})
}

// TestOptimizeConstFoldTable folds each supported binary operator.
func TestOptimizeConstFoldTable(t *testing.T) {
	intCases := []struct {
		op   string
		l, r int64
		want int64
	}{
		{"+", 2, 3, 5}, {"-", 5, 2, 3}, {"*", 4, 3, 12}, {"/", 9, 2, 4}, {"%", 9, 2, 1},
		{"<<", 1, 3, 8}, {">>", 16, 2, 4}, {"&", 6, 3, 2}, {"|", 4, 1, 5}, {"^", 6, 3, 5},
	}
	for _, tc := range intCases {
		got := ruleConstFold(&Binary{Op: tc.op, L: &IntLit{tc.l}, R: &IntLit{tc.r}})
		l, ok := got.(*IntLit)
		if !ok || l.Val != tc.want {
			t.Errorf("%d %s %d = %v, want %d", tc.l, tc.op, tc.r, got, tc.want)
		}
	}
	boolCases := []struct {
		op   string
		l, r int64
		want bool
	}{
		{"==", 1, 1, true}, {"!=", 1, 2, true}, {"<", 1, 2, true}, {"<=", 2, 2, true}, {">", 3, 2, true}, {">=", 2, 2, true},
	}
	for _, tc := range boolCases {
		got := ruleConstFold(&Binary{Op: tc.op, L: &IntLit{tc.l}, R: &IntLit{tc.r}})
		b, ok := got.(*BoolLit)
		if !ok || b.Val != tc.want {
			t.Errorf("%d %s %d = %v, want %v", tc.l, tc.op, tc.r, got, tc.want)
		}
	}
	// modulo-by-zero and out-of-range shifts are left intact
	if _, ok := ruleConstFold(&Binary{Op: "%", L: &IntLit{1}, R: &IntLit{0}}).(*Binary); !ok {
		t.Error("1 % 0 must not fold")
	}
	if _, ok := ruleConstFold(&Binary{Op: "<<", L: &IntLit{1}, R: &IntLit{99}}).(*Binary); !ok {
		t.Error("1 << 99 must not fold")
	}
	if _, ok := ruleConstFold(&Binary{Op: ">>", L: &IntLit{1}, R: &IntLit{-1}}).(*Binary); !ok {
		t.Error("1 >> -1 must not fold")
	}
	// a non-binary or non-literal node is returned unchanged
	if _, ok := ruleConstFold(&Ident{"a"}).(*Ident); !ok {
		t.Error("ruleConstFold should ignore non-binary nodes")
	}
	if _, ok := ruleConstFold(&Binary{Op: "+", L: &Ident{"a"}, R: &IntLit{1}}).(*Binary); !ok {
		t.Error("ruleConstFold needs two literals")
	}
}

// TestOptimizeStrengthAndIdentityEdges covers the literal-on-the-left and identity edge arms.
func TestOptimizeStrengthAndIdentityEdges(t *testing.T) {
	// 2 * x -> x << 1 (literal on the left)
	got := ruleStrengthMul(&Binary{Op: "*", L: &IntLit{2}, R: &Ident{"x"}})
	if b, ok := got.(*Binary); !ok || b.Op != "<<" {
		t.Errorf("2*x = %v, want x<<1", got)
	}
	// non-pow2 multiplier is untouched
	if b, ok := ruleStrengthMul(&Binary{Op: "*", L: &Ident{"x"}, R: &IntLit{3}}).(*Binary); !ok || b.Op != "*" {
		t.Error("x*3 must not strength-reduce")
	}
	// ruleStrengthDiv ignores non-division
	if _, ok := ruleStrengthDiv(&Binary{Op: "+", L: &Ident{"x"}, R: &IntLit{2}}).(*Binary); !ok {
		t.Error("strengthDiv should ignore +")
	}
	// identity: 0 + x -> x ; 1 * x -> x ; 0 * pure -> 0 ; x/1 -> x
	if _, ok := ruleIdentity(&Binary{Op: "+", L: &IntLit{0}, R: &Ident{"x"}}).(*Ident); !ok {
		t.Error("0 + x -> x")
	}
	if _, ok := ruleIdentity(&Binary{Op: "*", L: &IntLit{1}, R: &Ident{"x"}}).(*Ident); !ok {
		t.Error("1 * x -> x")
	}
	if l, ok := ruleIdentity(&Binary{Op: "*", L: &IntLit{0}, R: &Ident{"x"}}).(*IntLit); !ok || l.Val != 0 {
		t.Error("0 * x -> 0")
	}
	if _, ok := ruleIdentity(&Binary{Op: "/", L: &Ident{"x"}, R: &IntLit{1}}).(*Ident); !ok {
		t.Error("x / 1 -> x")
	}
	// isPure: unary of pure is pure; unary of impure is not
	if !isPure(&Unary{Op: "-", X: &Ident{"a"}}) {
		t.Error("-a is pure")
	}
	if isPure(&Unary{Op: "-", X: &Call{Callee: "f"}}) {
		t.Error("-f() is not pure")
	}
}
