package main

import (
	"os"
	"path/filepath"
	"testing"
)

// equivProg parses + checks a program and runs the bounded equivalence oracle on f vs g.
func equivProg(t *testing.T, f, g string, decls ...string) equivResult {
	t.Helper()
	nd := make([]string, len(decls))
	for i, d := range decls {
		nd[i] = normalize(d)
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return checkEquiv(prog, c, f, g)
}

// TestEquivProvesEquivalent: a loop and its closed form agree on every input in the
// bounded domain, so the oracle reports equivalent-bounded (bounded ints ⇒ not total).
func TestEquivProvesEquivalent(t *testing.T) {
	r := equivProg(t, "sum_loop", "sum_formula",
		`func sum_loop(n) (r) { r = 0  i := 1  for i <= n { r = r + i  i = i + 1 } }`,
		`func sum_formula(n) (r) { r = n * (n + 1) / 2 }`,
		`func main() { println(str(sum_loop(4) + sum_formula(4))) }`,
	)
	if r.Verdict != "equivalent-bounded" {
		t.Fatalf("verdict %q (compared %d), want equivalent-bounded", r.Verdict, r.Compared)
	}
	if r.Compared == 0 {
		t.Fatal("expected at least one compared input")
	}
}

// TestEquivProvesEquivalentTotal: over a finite (bool) domain the whole space is
// enumerated, so agreement is a total `equivalent`, not merely bounded.
func TestEquivProvesEquivalentTotal(t *testing.T) {
	r := equivProg(t, "id_a", "id_b",
		`func id_a(p) (r) { r = 0  if p { r = 1 } }`,
		`func id_b(p) (r) { r = 1  if !p { r = 0 } }`,
		`func main() { println(str(id_a(true) + id_b(false))) }`,
	)
	if r.Verdict != "equivalent" {
		t.Fatalf("verdict %q, want equivalent (finite bool domain fully covered)", r.Verdict)
	}
}

// TestEquivCatchesDivergence: a wrong "optimization" (n*n/2 for the triangular sum)
// is caught with the exact diverging input and both values.
func TestEquivCatchesDivergence(t *testing.T) {
	r := equivProg(t, "sum_loop", "sum_bad",
		`func sum_loop(n) (r) { r = 0  i := 1  for i <= n { r = r + i  i = i + 1 } }`,
		`func sum_bad(n) (r) { r = n * n / 2 }`,
		`func main() { println(str(sum_loop(3) + sum_bad(3))) }`,
	)
	if r.Verdict != "diverges" {
		t.Fatalf("verdict %q, want diverges", r.Verdict)
	}
	if r.Bind == "" || r.FVal == r.GVal {
		t.Fatalf("expected a concrete counterexample, got %+v", r)
	}
}

// TestEquivMultiParam: a two-argument algebraic rewrite (a*2 + a*3 + b - b == a*5) is
// proven over the full cartesian product of the bounded domain.
func TestEquivMultiParam(t *testing.T) {
	r := equivProg(t, "poly_orig", "poly_opt",
		`func poly_orig(a, b) (r) { r = a * 2 + a * 3 + b - b }`,
		`func poly_opt(a, b) (r) { r = a * 5 }`,
		`func main() { println(str(poly_orig(2, 3) + poly_opt(2, 3))) }`,
	)
	if r.Verdict != "equivalent-bounded" {
		t.Fatalf("verdict %q, want equivalent-bounded", r.Verdict)
	}
	if r.Compared < 25 {
		t.Errorf("expected the full 5x5 product compared, got %d", r.Compared)
	}
}

// TestEquivInconclusive: mismatched arity, an unknown function, and an unbounded param
// domain all yield an honest inconclusive rather than a false "equivalent".
func TestEquivInconclusive(t *testing.T) {
	// arity mismatch
	r := equivProg(t, "one", "two",
		`func one(a) (r) { r = a }`,
		`func two(a, b) (r) { r = a + b }`,
		`func main() { println(str(one(1) + two(1, 2))) }`,
	)
	if r.Verdict != "inconclusive" {
		t.Errorf("arity mismatch: verdict %q, want inconclusive", r.Verdict)
	}
	// unknown function
	r = equivProg(t, "one", "ghost",
		`func one(a) (r) { r = a }`,
		`func main() { println(str(one(1))) }`,
	)
	if r.Verdict != "inconclusive" {
		t.Errorf("unknown g: verdict %q, want inconclusive", r.Verdict)
	}
}

// TestEquivCommand drives the CLI end to end on equivalent programs (text + --json) and
// its error paths. The diverging exit(1) path is covered by checkEquiv unit tests above.
func TestEquivCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e.mfl")
	src := "func f(a) (r) { r = a + a }\nfunc g(a) (r) { r = a * 2 }\nfunc main() { println(str(f(3) + g(3))) }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdEquiv([]string{"f", "g", path}); err != nil {
		t.Fatalf("equiv (text): %v", err)
	}
	if err := cmdEquiv([]string{"f", "g", path, "--json"}); err != nil {
		t.Fatalf("equiv --json: %v", err)
	}
	if err := cmdEquiv([]string{"f", path}); err == nil {
		t.Fatal("expected usage error with only one function name")
	}
	if err := cmdEquiv([]string{"f", "g"}); err == nil {
		t.Fatal("expected usage error with no source file")
	}
	if err := cmdEquiv([]string{"f", "g", filepath.Join(dir, "missing.mfl")}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	bad := filepath.Join(dir, "bad.mfl")
	os.WriteFile(bad, []byte("func main() { x := }\n"), 0o644)
	if err := cmdEquiv([]string{"f", "g", bad}); err == nil {
		t.Fatal("expected a parse/type error for malformed source")
	}
}

// TestEquivPrintRendering covers every branch of the text renderer.
func TestEquivPrintRendering(t *testing.T) {
	printEquiv(equivResult{F: "f", G: "g", Verdict: "equivalent", Compared: 2})
	printEquiv(equivResult{F: "f", G: "g", Verdict: "equivalent-bounded", Compared: 5})
	printEquiv(equivResult{F: "f", G: "g", Verdict: "diverges", Bind: "1", FVal: "1", GVal: "0"})
	printEquiv(equivResult{F: "f", G: "g", Verdict: "inconclusive"})
}
