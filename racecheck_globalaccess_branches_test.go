package main

import "testing"

// racecheck_analysis_test.go already covers the common-case branches of
// collectGlobalAccess/collectCallGraph (Ident/Binary/Call reads; AssignStmt/
// ExprStmt/IfStmt/GoStmt/SendStmt walks). This file covers the remaining
// expr/stmt kinds those functions switch on, plus blockOf's RangeStmt/ArenaStmt
// cases and mainPostGlobals' pre-spawn nested-block descent.

func TestCollectGlobalAccessRemainingExprKinds(t *testing.T) {
	gset := map[string]bool{"g": true}
	local := map[string]bool{}

	var reads []string
	sink := func(name string, read, write bool) {
		if read {
			reads = append(reads, name)
		}
	}

	body := []Stmt{
		&ExprStmt{X: &Index{X: &Ident{Name: "g"}, Idx: &IntLit{Val: 0}}},
		&ExprStmt{X: &FieldAccess{X: &Ident{Name: "g"}, Name: "f"}},
		&ExprStmt{X: &Unary{Op: "-", X: &Ident{Name: "g"}}},
		&ExprStmt{X: &SliceLit{Elems: []Expr{&Ident{Name: "g"}}}},
		&ExprStmt{X: &StructLit{Type: "P", Vals: []Expr{&Ident{Name: "g"}}}},
		&IfStmt{Cond: nil, Then: nil}, // rd(nil) must not panic
	}

	collectGlobalAccess(body, gset, local, sink)

	if len(reads) < 5 {
		t.Fatalf("expected at least 5 reads of g (index/field/unary/slice/struct), got %v", reads)
	}
	for _, r := range reads {
		if r != "g" {
			t.Fatalf("unexpected read target %q, want only \"g\"", r)
		}
	}
}

func TestCollectGlobalAccessRemainingStmtKinds(t *testing.T) {
	gset := map[string]bool{"g": true}
	local := map[string]bool{}

	var events []string
	sink := func(name string, read, write bool) {
		switch {
		case write:
			events = append(events, "write:"+name)
		case read:
			events = append(events, "read:"+name)
		}
	}

	body := []Stmt{
		&MultiAssign{Names: []string{"a", "b"}, Op: ":=", Rhs: []Expr{&Ident{Name: "g"}}},
		&IndexAssign{Target: &Index{X: &Ident{Name: "g"}, Idx: &IntLit{Val: 0}}, Val: &IntLit{Val: 1}},
		&FieldAssign{Target: &FieldAccess{X: &Ident{Name: "g"}, Name: "f"}, Val: &IntLit{Val: 1}},
		&ReturnStmt{Vals: []Expr{&Ident{Name: "g"}}},
		&WhileStmt{Cond: &Ident{Name: "g"}, Body: []Stmt{&ExprStmt{X: &Ident{Name: "g"}}}},
		&RangeStmt{X: &Ident{Name: "g"}, Body: []Stmt{&ExprStmt{X: &Ident{Name: "g"}}}},
		&ArenaStmt{Body: []Stmt{&ExprStmt{X: &Ident{Name: "g"}}}},
	}

	collectGlobalAccess(body, gset, local, sink)

	wantWrite := map[string]bool{"write:g": false} // IndexAssign/FieldAssign on g are whole-cell paths, not plain idents
	for _, e := range events {
		if e == "write:g" {
			wantWrite["write:g"] = true
		}
	}
	// IndexAssign/FieldAssign target g via placeOf, so they DO report a write.
	if !wantWrite["write:g"] {
		t.Errorf("expected at least one write:g event from IndexAssign/FieldAssign, got %v", events)
	}

	readCount := 0
	for _, e := range events {
		if e == "read:g" {
			readCount++
		}
	}
	// MultiAssign rhs, ReturnStmt val, while cond+body, range X+body, arena body: >= 6
	if readCount < 6 {
		t.Errorf("expected >= 6 reads of g across MultiAssign/ReturnStmt/While/Range/Arena, got %d (%v)", readCount, events)
	}
}

func TestCollectCallGraphRemainingExprKinds(t *testing.T) {
	var normal []string
	var gos []goSite

	body := []Stmt{
		&ExprStmt{X: &Unary{Op: "-", X: &Call{Callee: "unaryArg"}}},
		&ExprStmt{X: &Index{X: &Call{Callee: "indexBase"}, Idx: &Call{Callee: "indexArg"}}},
		&ExprStmt{X: &FieldAccess{X: &Call{Callee: "fieldBase"}, Name: "f"}},
		&ExprStmt{X: &SliceLit{Elems: []Expr{&Call{Callee: "sliceElem"}}}},
		&ExprStmt{X: &StructLit{Type: "P", Vals: []Expr{&Call{Callee: "structVal"}}}},
	}

	collectCallGraph(body, false, &normal, &gos)

	want := []string{"unaryArg", "indexBase", "indexArg", "fieldBase", "sliceElem", "structVal"}
	for _, w := range want {
		found := false
		for _, got := range normal {
			if got == w {
				found = true
			}
		}
		if !found {
			t.Errorf("collectCallGraph: normal callees %v missing %q", normal, w)
		}
	}
}

func TestCollectCallGraphRemainingStmtKinds(t *testing.T) {
	var normal []string
	var gos []goSite

	body := []Stmt{
		&MultiAssign{Names: []string{"a", "b"}, Op: ":=", Rhs: []Expr{&Call{Callee: "multiAssignRhs"}}},
		&IndexAssign{Target: &Index{X: &Ident{Name: "x"}, Idx: &IntLit{Val: 0}}, Val: &Call{Callee: "indexAssignVal"}},
		&FieldAssign{Target: &FieldAccess{X: &Ident{Name: "x"}, Name: "f"}, Val: &Call{Callee: "fieldAssignVal"}},
		&ReturnStmt{Vals: []Expr{&Call{Callee: "returnVal"}}},
		&RangeStmt{X: &Ident{Name: "xs"}, Body: []Stmt{&GoStmt{Call: &Call{Callee: "rangedWorker"}}}},
		&ArenaStmt{Body: []Stmt{&GoStmt{Call: &Call{Callee: "arenaWorker"}}}},
	}

	collectCallGraph(body, false, &normal, &gos)

	want := []string{"multiAssignRhs", "indexAssignVal", "fieldAssignVal", "returnVal"}
	for _, w := range want {
		found := false
		for _, got := range normal {
			if got == w {
				found = true
			}
		}
		if !found {
			t.Errorf("collectCallGraph: normal callees %v missing %q", normal, w)
		}
	}

	bySite := map[string]bool{}
	for _, g := range gos {
		bySite[g.callee] = g.inLoop
	}
	if inLoop, ok := bySite["rangedWorker"]; !ok || !inLoop {
		t.Errorf("collectCallGraph: 'rangedWorker' inside a range must have inLoop=true, got %v (present=%v)", inLoop, ok)
	}
	if inLoop, ok := bySite["arenaWorker"]; !ok || inLoop {
		t.Errorf("collectCallGraph: 'arenaWorker' inside an arena (not a loop) must inherit inLoop=false, got %v (present=%v)", inLoop, ok)
	}
}

func TestBlockOfRangeAndArena(t *testing.T) {
	rangeBody := []Stmt{&AssignStmt{Name: "a", Op: ":=", Val: &IntLit{Val: 1}}}
	if body, ok := blockOf(&RangeStmt{Body: rangeBody}); !ok || len(body) != 1 {
		t.Fatalf("blockOf(RangeStmt) = (%v, %v), want its body", body, ok)
	}

	arenaBody := []Stmt{&AssignStmt{Name: "b", Op: ":=", Val: &IntLit{Val: 2}}}
	if body, ok := blockOf(&ArenaStmt{Body: arenaBody}); !ok || len(body) != 1 {
		t.Fatalf("blockOf(ArenaStmt) = (%v, %v), want its body", body, ok)
	}
}

// mainPostGlobals must descend into a block that precedes the first `go` spawn
// (to find a spawn nested inside it) without collecting the accesses inside
// that pre-spawn block itself.
func TestMainPostGlobalsDescendsPreSpawnBlock(t *testing.T) {
	gset := map[string]bool{"g": true}
	local := map[string]bool{}

	body := []Stmt{
		&IfStmt{
			Cond: &Ident{Name: "cond"},
			Then: []Stmt{
				&ExprStmt{X: &Ident{Name: "g"}}, // pre-spawn access inside the nested block: must NOT be collected
				&GoStmt{Call: &Call{Callee: "worker"}},
			},
		},
		&ExprStmt{X: &Ident{Name: "g"}}, // post-spawn (top-level, after the if): must be collected
	}

	got := mainPostGlobals(body, gset, local)

	acc := got["g"]
	if acc == nil || !acc.read {
		t.Fatalf("mainPostGlobals: expected a post-spawn read of g, got %v", got)
	}
}
