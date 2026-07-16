package main

import (
	"strings"
	"testing"
)

// TestFalsifyContracts is the Phase 3 gate: declarative `requires`/`ensures`
// clauses. `requires` filters the input domain (a precondition-violating input is
// not a bug here); a satisfied-precondition input that makes an `ensures` false is
// a FALS010 counterexample.
func TestFalsifyContracts(t *testing.T) {
	cases := []struct {
		name     string
		fn       string
		seed     string
		target   string
		wantCode string // "" => no finding
		wantExpr string
	}{
		{
			name:   "requires suppresses div-by-zero",
			fn:     `func div(a,b) requires b != 0 { return a/b }`,
			seed:   `func main(){println(str(div(6,2)))}`,
			target: "div",
		},
		{
			name:     "without requires, div-by-zero found",
			fn:       `func divx(a,b){ return a/b }`,
			seed:     `func main(){println(str(divx(6,2)))}`,
			target:   "divx",
			wantCode: "FALS002",
			wantExpr: "a / b",
		},
		{
			name:     "ensures violated -> FALS010",
			fn:       `func bad(x) (r) ensures r >= x { return x - 1 }`,
			seed:     `func main(){println(str(bad(5)))}`,
			target:   "bad",
			wantCode: "FALS010",
			wantExpr: "r >= x",
		},
		{
			name:   "ensures holds (abs) -> clean",
			fn:     `func myabs(x) (r) ensures r >= 0 { if x < 0 { return 0 - x } return x }`,
			seed:   `func main(){println(str(myabs(-3)))}`,
			target: "myabs",
		},
		{
			name:   "requires + ensures both hold",
			fn:     `func safediv(a,b) (r) requires b != 0  ensures r == a/b { return a/b }`,
			seed:   `func main(){println(str(safediv(6,2)))}`,
			target: "safediv",
		},
		{
			name:     "requires narrows but a bug remains within the precondition",
			fn:       `func recip(x) (r) requires x > 0  ensures r > 0 { return 10/x - 5 }`,
			seed:     `func main(){println(str(recip(1)))}`,
			target:   "recip",
			wantCode: "FALS010",
			wantExpr: "r > 0",
		},
		{
			name:   "two requires clauses both filter",
			fn:     `func between(x) (r) requires x > 0  requires x < 3  ensures r == x { return x }`,
			seed:   `func main(){println(str(between(1)))}`,
			target: "between",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := ParseProgram([]string{tc.fn, tc.seed})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			c, err := Check(prog)
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			var got *falsifyFinding
			for _, f := range detectFalsifiable(prog, c) {
				if f.Decl == tc.target {
					g := f
					got = &g
					break
				}
			}
			if tc.wantCode == "" {
				if got != nil {
					t.Fatalf("expected no finding, got %+v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %s for %s, got none", tc.wantCode, tc.target)
			}
			if got.Code != tc.wantCode || got.Expr != tc.wantExpr {
				t.Fatalf("code/expr = %s/%q, want %s/%q", got.Code, got.Expr, tc.wantCode, tc.wantExpr)
			}
			t.Logf("FALSIFIED %s: %s at `%s` when %s", tc.target, got.Prop, got.Expr, got.Bind)
		})
	}
}

// TestContractEdges covers the FALS010 diagnostic + inconclusive predicates.
func TestContractEdges(t *testing.T) {
	// FALS010 diagnostic rendering.
	ff := falsifyFinding{Decl: "bad", Code: "FALS010", Prop: "postcondition violated", Expr: "r >= x", Bind: "x=0"}
	d := ff.toDiagnostic()
	if d.Phase != "falsify" || d.Code != "FALS010" || d.Severity != "warning" {
		t.Fatalf("diag = %+v", d)
	}
	if !strings.Contains(d.Message, "postcondition violated at `r >= x` when x=0") {
		t.Fatalf("message = %q", d.Message)
	}

	// An `ensures` the interpreter can't evaluate (references an unmodeled builtin,
	// str) is inconclusive, never a false FALS010.
	prog, err := ParseProgram([]string{
		`func q(x) (r) ensures str(r) != "" { return x }`,
		`func main(){println(str(q(1)))}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, f := range detectFalsifiable(prog, c) {
		if f.Decl == "q" && f.Code == "FALS010" {
			t.Fatalf("inconclusive ensures must not yield a finding: %+v", f)
		}
	}

	// the SECOND ensures clause is the one that fails -> reported with its expr.
	prog2, err := ParseProgram([]string{
		`func f2(x) (r) ensures r >= 0  ensures r < 10 { return x }`,
		`func main(){println(str(f2(1)))}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c2, err := Check(prog2)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	var got *falsifyFinding
	for _, f := range detectFalsifiable(prog2, c2) {
		if f.Decl == "f2" {
			g := f
			got = &g
		}
	}
	// x ranges over {0,1,-1,2,3}; the first violating input is x=-1 (r>=0 fails)?
	// No: domain order is 0,1,-1,2,3 -> x=0 gives r=0: r>=0 ok, r<10 ok. Eventually
	// none of {0,1,-1,2,3} exceed 10, and -1 fails r>=0. First fail is x=-1 on the
	// first clause.
	if got == nil || got.Code != "FALS010" {
		t.Fatalf("expected FALS010 for f2, got %+v", got)
	}
	// the emitted repro is a runnable program calling the target with the input.
	repro := reproProgram([]string{`func f2(x) (r) ensures r >= 0  ensures r < 10 { return x }`}, "f2", got.args)
	if !strings.Contains(repro, "f2(") || !strings.Contains(repro, "func main()") {
		t.Fatalf("repro missing call/main:\n%s", repro)
	}
}

// TestContractParsing pins the parser: requires/ensures land on the FuncDecl and
// the body still parses.
func TestContractParsing(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func clamp(x,lo,hi) (r) requires lo <= hi  ensures r >= lo  ensures r <= hi { r = x  if r < lo { r = lo }  if r > hi { r = hi }  return r }`,
		`func main(){println(str(clamp(5,0,10)))}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var clamp *FuncDecl
	for _, f := range prog.Funcs {
		if f.Name == "clamp" {
			clamp = f
		}
	}
	if clamp == nil {
		t.Fatal("clamp not parsed")
	}
	if len(clamp.Requires) != 1 || len(clamp.Ensures) != 2 {
		t.Fatalf("requires=%d ensures=%d, want 1/2", len(clamp.Requires), len(clamp.Ensures))
	}
	if got := exprStr(clamp.Requires[0]); got != "lo <= hi" {
		t.Fatalf("requires[0] = %q", got)
	}
	if got := exprStr(clamp.Ensures[1]); got != "r <= hi" {
		t.Fatalf("ensures[1] = %q", got)
	}
	// a contract-free function keeps empty slices.
	for _, f := range prog.Funcs {
		if f.Name == "main" && (len(f.Requires) != 0 || len(f.Ensures) != 0) {
			t.Fatalf("main should have no contracts")
		}
	}
	_ = strings.TrimSpace
}
