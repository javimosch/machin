package main

import "testing"

func TestCollectClosureVars(t *testing.T) {
	innerMC := &MakeClosure{FuncName: "lambda_0", Captures: []string{"x"}}
	shadowedMC := &MakeClosure{FuncName: "lambda_1", Captures: []string{"y"}}
	body := []Stmt{
		&AssignStmt{Name: "f", Op: ":=", Val: innerMC},
		&IfStmt{
			Then: []Stmt{&AssignStmt{Name: "g", Op: ":=", Val: shadowedMC}},
		},
		&WhileStmt{
			Body: []Stmt{&AssignStmt{Name: "h", Op: ":=", Val: &MakeClosure{FuncName: "lambda_2"}}},
		},
		&RangeStmt{
			Body: []Stmt{&AssignStmt{Name: "k", Op: ":=", Val: &MakeClosure{FuncName: "lambda_3"}}},
		},
		&ArenaStmt{
			Body: []Stmt{&AssignStmt{Name: "m", Op: ":=", Val: &MakeClosure{FuncName: "lambda_4"}}},
		},
		&AssignStmt{Name: "notClosure", Op: ":=", Val: &IntLit{Val: 1}},
	}

	into := map[string]*MakeClosure{}
	collectClosureVars(body, into)

	want := []string{"f", "g", "h", "k", "m"}
	for _, name := range want {
		if into[name] == nil {
			t.Errorf("collectClosureVars: missing MakeClosure for %q, got %v", name, into)
		}
	}
	if into["f"] != innerMC {
		t.Errorf("collectClosureVars: %q maps to %v, want the original MakeClosure pointer", "f", into["f"])
	}
	if _, ok := into["notClosure"]; ok {
		t.Errorf("collectClosureVars: non-closure assignment must not be recorded, got %v", into)
	}
}

func TestCollectGlobalAccess(t *testing.T) {
	gset := map[string]bool{"g": true, "shadowed": true}
	local := map[string]bool{"shadowed": true}

	var events []string
	sink := func(name string, read, write bool) {
		switch {
		case read:
			events = append(events, "read:"+name)
		case write:
			events = append(events, "write:"+name)
		}
	}

	body := []Stmt{
		&AssignStmt{Name: "g", Op: "=", Val: &IntLit{Val: 1}},          // whole-cell write
		&AssignStmt{Name: "shadowed", Op: "=", Val: &Ident{Name: "g"}}, // shadowed by local: no write event, but rhs reads g
		&ExprStmt{X: &Ident{Name: "g"}},
		&IfStmt{
			Cond: &Ident{Name: "g"},
			Then: []Stmt{&ExprStmt{X: &Ident{Name: "g"}}},
		},
	}

	collectGlobalAccess(body, gset, local, sink)

	wantReads := 0
	wantWrites := 0
	for _, e := range events {
		switch e {
		case "read:g":
			wantReads++
		case "write:g":
			wantWrites++
		case "write:shadowed", "read:shadowed":
			t.Errorf("collectGlobalAccess: shadowed local %q must not be reported, got event %q", "shadowed", e)
		}
	}
	if wantWrites != 1 {
		t.Errorf("collectGlobalAccess: want 1 write of g, got %d (%v)", wantWrites, events)
	}
	if wantReads != 4 {
		t.Errorf("collectGlobalAccess: want 4 reads of g (rhs of shadowed=, bare ExprStmt, if-cond, if-then), got %d (%v)", wantReads, events)
	}
}

func TestCollectCallGraph(t *testing.T) {
	var normal []string
	var gos []goSite

	body := []Stmt{
		&ExprStmt{X: &Call{Callee: "topLevel"}},
		&GoStmt{Call: &Call{Callee: "worker", Args: []Expr{&Call{Callee: "argCall"}}}},
		&IfStmt{
			Then: []Stmt{&GoStmt{Call: &Call{Callee: "thenWorker"}}},
			Else: []Stmt{&ExprStmt{X: &Call{Callee: "elseCallee"}}},
		},
		&WhileStmt{
			Body: []Stmt{&GoStmt{Call: &Call{Callee: "loopedWorker"}}},
		},
	}

	collectCallGraph(body, false, &normal, &gos)

	wantNormal := map[string]bool{"topLevel": true, "argCall": true, "elseCallee": true}
	for n := range wantNormal {
		found := false
		for _, got := range normal {
			if got == n {
				found = true
			}
		}
		if !found {
			t.Errorf("collectCallGraph: normal callees %v missing %q", normal, n)
		}
	}

	bySite := map[string]bool{}
	for _, g := range gos {
		bySite[g.callee] = g.inLoop
	}
	if inLoop, ok := bySite["worker"]; !ok || inLoop {
		t.Errorf("collectCallGraph: top-level go site 'worker' should have inLoop=false, got %v (present=%v)", inLoop, ok)
	}
	if inLoop, ok := bySite["thenWorker"]; !ok || inLoop {
		t.Errorf("collectCallGraph: 'thenWorker' inherits inLoop=false from an if-branch, got %v (present=%v)", inLoop, ok)
	}
	if inLoop, ok := bySite["loopedWorker"]; !ok || !inLoop {
		t.Errorf("collectCallGraph: 'loopedWorker' inside a while must have inLoop=true, got %v (present=%v)", inLoop, ok)
	}
}
