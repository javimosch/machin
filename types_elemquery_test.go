package main

import "testing"

// TestElemQueriesOnSlice covers ElemKindOf, ElemCType, and ElemTypeString
// against a slice-typed node's element (the KSlice/KChan branch of each).
func TestElemQueriesOnSlice(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { xs := []int{1, 2, 3} for _, v := range xs { println(v) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	var inst string
	var xs Expr
	for _, r := range c.Reps() {
		if c.SrcFunc(r).Name != "main" {
			continue
		}
		inst = r
		for _, stmt := range c.SrcFunc(r).Body {
			if rs, ok := stmt.(*RangeStmt); ok {
				xs = rs.X
			}
		}
	}
	if inst == "" || xs == nil {
		t.Fatal("no main instance / range base node found")
	}

	if got := c.ElemKindOf(inst, xs); got != KInt {
		t.Errorf("ElemKindOf(xs) = %v, want %v", got, KInt)
	}
	if got := c.ElemCType(inst, xs); got != "int64_t" {
		t.Errorf("ElemCType(xs) = %q, want %q", got, "int64_t")
	}
	if got := c.ElemTypeString(inst, xs); got != "int" {
		t.Errorf("ElemTypeString(xs) = %q, want %q", got, "int")
	}
}

// TestElemQueriesFallbackOnNonSlice covers the default-value fallback branch
// of ElemKindOf/ElemCType/ElemTypeString when the node isn't a slice or chan.
func TestElemQueriesFallbackOnNonSlice(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { n := 5 println(n) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	var inst string
	var n Expr
	for _, r := range c.Reps() {
		if c.SrcFunc(r).Name != "main" {
			continue
		}
		inst = r
		for _, stmt := range c.SrcFunc(r).Body {
			if as, ok := stmt.(*AssignStmt); ok && as.Name == "n" {
				n = as.Val
			}
		}
	}
	if inst == "" || n == nil {
		t.Fatal("no main instance / assign node found")
	}

	if got := c.ElemKindOf(inst, n); got != KInt {
		t.Errorf("ElemKindOf(n) fallback = %v, want %v", got, KInt)
	}
	if got := c.ElemCType(inst, n); got != "int64_t" {
		t.Errorf("ElemCType(n) fallback = %q, want %q", got, "int64_t")
	}
	if got := c.ElemTypeString(inst, n); got != "int" {
		t.Errorf("ElemTypeString(n) fallback = %q, want %q", got, "int")
	}
}

// TestParamElemCType covers ParamElemCType's slice-param branch and its
// int64_t fallback for a non-slice param, on the same function's two
// monomorphized instances.
func TestParamElemCType(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func identity(xs) { return xs }`,
		`func main() { s := []int{1, 2, 3} a := identity(s) b := identity(7) println(len(a)) println(b) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	var sliceInst, intInst string
	for _, r := range c.Reps() {
		if c.SrcFunc(r).Name != "identity" {
			continue
		}
		if c.ParamKind(r, 0) == KSlice {
			sliceInst = r
		} else {
			intInst = r
		}
	}
	if sliceInst == "" || intInst == "" {
		t.Fatal("expected two monomorphized instances of identity (slice and int)")
	}

	if got := c.ParamElemCType(sliceInst, 0); got != "int64_t" {
		t.Errorf("ParamElemCType(slice param) = %q, want %q", got, "int64_t")
	}
	if got := c.ParamElemCType(intInst, 0); got != "int64_t" {
		t.Errorf("ParamElemCType(int param) fallback = %q, want %q", got, "int64_t")
	}
}
