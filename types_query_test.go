package main

import "testing"

// TestGlobalQueries covers IsGlobal and GlobalKind: a declared package global
// must be reported as global with its inferred kind, and an unrelated name
// must not be mistaken for one.
func TestGlobalQueries(t *testing.T) {
	prog, err := ParseProgram([]string{
		"var total = 3",
		"func main() { total = 4 }",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	if !c.IsGlobal("total") {
		t.Error("IsGlobal(\"total\") = false, want true")
	}
	if c.IsGlobal("notAGlobal") {
		t.Error("IsGlobal(\"notAGlobal\") = true, want false")
	}
	if got := c.GlobalKind("total"); got != KInt {
		t.Errorf("GlobalKind(\"total\") = %v, want %v", got, KInt)
	}
}

// TestParamAndRetKind covers ParamKind and RetKindAt against a function whose
// param and named return have distinct kinds.
func TestParamAndRetKind(t *testing.T) {
	prog, err := ParseProgram([]string{
		"func square(x) (y) { y = x * x  return y }",
		"func main() { println(square(2)) }",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	var inst string
	for _, r := range c.Reps() {
		if c.SrcFunc(r).Name == "square" {
			inst = r
			break
		}
	}
	if inst == "" {
		t.Fatal("no monomorphized instance found for square")
	}

	if got := c.ParamKind(inst, 0); got != KInt {
		t.Errorf("ParamKind(square, 0) = %v, want %v", got, KInt)
	}
	if got := c.RetKindAt(inst, 0); got != KInt {
		t.Errorf("RetKindAt(square, 0) = %v, want %v", got, KInt)
	}
}

// TestClosureInst covers ClosureInst: a MakeClosure node produced by lambda-
// lifting must resolve to the lifted lambda's instance, matching the C name
// codegen would call.
func TestClosureInst(t *testing.T) {
	prog, err := ParseProgram([]string{
		"func main() { f := func() { return 7 }  println(f()) }",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	var encl string
	var mc *MakeClosure
	for _, r := range c.Reps() {
		for _, stmt := range c.SrcFunc(r).Body {
			if as, ok := stmt.(*AssignStmt); ok {
				if m, ok := as.Val.(*MakeClosure); ok {
					encl, mc = r, m
				}
			}
		}
	}
	if mc == nil {
		t.Fatal("no MakeClosure node found after lambda-lifting")
	}

	inst := c.ClosureInst(encl, mc)
	if inst == "" {
		t.Fatal("ClosureInst returned empty instance name")
	}
	if got, want := c.CName(inst), c.ClosureCName(encl, mc); got != want {
		t.Errorf("CName(ClosureInst(...)) = %q, want %q (ClosureCName)", got, want)
	}
}

// TestIsLocalAndHasMain covers IsLocal (checking if a name is a local var in
// an instance), HasMain (checking if program has a main function), and
// IsTopFunc (checking if a name is defined at the top level).
func TestIsLocalAndHasMain(t *testing.T) {
	prog, err := ParseProgram([]string{
		"var globalX = 1",
		"func helper(x) { y := x + 1  return y }",
		"func main() { z := helper(2)  println(z) }",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	// HasMain must return true when main() is defined
	if !c.HasMain() {
		t.Error("HasMain() = false, want true")
	}

	// IsTopFunc must identify functions defined at top level
	if !c.IsTopFunc("main") {
		t.Error("IsTopFunc(\"main\") = false, want true")
	}
	if !c.IsTopFunc("helper") {
		t.Error("IsTopFunc(\"helper\") = false, want true")
	}
	if c.IsTopFunc("unknownFunc") {
		t.Error("IsTopFunc(\"unknownFunc\") = true, want false")
	}

	// IsLocal must identify locals within a function instance
	var mainInst string
	for _, r := range c.Reps() {
		if c.SrcFunc(r).Name == "main" {
			mainInst = r
			break
		}
	}
	if mainInst == "" {
		t.Fatal("no main instance found")
	}

	if c.IsLocal(mainInst, "globalX") {
		t.Error("IsLocal(main, \"globalX\") = true, want false (globalX is global)")
	}
	if !c.IsLocal(mainInst, "z") {
		t.Error("IsLocal(main, \"z\") = false, want true (z is declared in main)")
	}
}
