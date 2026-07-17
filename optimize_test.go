package main

import (
	"os"
	"path/filepath"
	"testing"
)

// optProg parses + checks a program and runs the oracle-gated optimizer.
func optProg(t *testing.T, decls ...string) optReport {
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
	return optimizeProgram(prog, c)
}

func funcResult(rep optReport, fn string) (optFuncResult, bool) {
	for _, f := range rep.Funcs {
		if f.Fn == fn {
			return f, true
		}
	}
	return optFuncResult{}, false
}

// TestOptimizeStrengthReduction: x*8 lowers to x<<3, proven equivalent, and the optimized
// source is emitted.
func TestOptimizeStrengthReduction(t *testing.T) {
	rep := optProg(t,
		`func scale(x) (r) { r = x * 8 }`,
		`func main() { println(str(scale(2))) }`,
	)
	f, ok := funcResult(rep, "scale")
	if !ok || len(f.Passes) != 1 || f.Passes[0].Rule != "strength-reduction" {
		t.Fatalf("scale passes = %+v", f.Passes)
	}
	if f.Passes[0].Verdict != "equivalent-bounded" {
		t.Errorf("verdict %q, want equivalent-bounded", f.Passes[0].Verdict)
	}
	if f.Optimized != "func scale(x) (r) { r = x << 3 }" {
		t.Errorf("optimized = %q", f.Optimized)
	}
}

// TestOptimizeConstantFolding: a + 2*3 folds to a + 6.
func TestOptimizeConstantFolding(t *testing.T) {
	rep := optProg(t,
		`func fold(a) (r) { r = a + 2 * 3 }`,
		`func main() { println(str(fold(1))) }`,
	)
	f, _ := funcResult(rep, "fold")
	if f.Optimized != "func fold(a) (r) { r = a + 6 }" {
		t.Errorf("optimized = %q", f.Optimized)
	}
}

// TestOptimizeIdentity: a*1 + 0 collapses to a.
func TestOptimizeIdentity(t *testing.T) {
	rep := optProg(t,
		`func ident(a) (r) { r = a * 1 + 0 }`,
		`func main() { println(str(ident(5))) }`,
	)
	f, _ := funcResult(rep, "ident")
	if f.Optimized != "func ident(a) (r) { r = a }" {
		t.Errorf("optimized = %q", f.Optimized)
	}
}

// TestOptimizeRejectsUnsoundRewrite: the speculative signed /2^k -> >>k rewrite is REFUTED
// by the oracle with the exact negative counterexample, and is NOT applied.
func TestOptimizeRejectsUnsoundRewrite(t *testing.T) {
	rep := optProg(t,
		`func halve(x) (r) { r = x / 2 }`,
		`func main() { println(str(halve(9))) }`,
	)
	f, _ := funcResult(rep, "halve")
	if len(f.Passes) != 1 || f.Passes[0].Verdict != "diverges" {
		t.Fatalf("halve passes = %+v, want a single diverges", f.Passes)
	}
	if f.Optimized != "" {
		t.Errorf("a refuted rewrite must not emit optimized source, got %q", f.Optimized)
	}
	if rep.Rejected != 1 || rep.Applied != 0 {
		t.Errorf("applied=%d rejected=%d, want 0/1", rep.Applied, rep.Rejected)
	}
	if f.Passes[0].Detail == "" {
		t.Error("expected a counterexample detail")
	}
}

// TestOptimizeCumulative: identity elimination and strength reduction both apply to one
// function, and both carry a proof.
func TestOptimizeCumulative(t *testing.T) {
	rep := optProg(t,
		`func mix(a, b) (r) { r = a * 4 + b * 0 }`,
		`func main() { println(str(mix(3, 4))) }`,
	)
	f, _ := funcResult(rep, "mix")
	if f.applied < 2 {
		t.Fatalf("expected >=2 applied passes, got %+v", f.Passes)
	}
	if f.Optimized != "func mix(a, b) (r) { r = a << 2 }" {
		t.Errorf("optimized = %q", f.Optimized)
	}
}

// TestOptimizeNoChange: a function with nothing to simplify produces no passes.
func TestOptimizeNoChange(t *testing.T) {
	rep := optProg(t,
		`func f(a, b) (r) { r = a + b }`,
		`func main() { println(str(f(1, 2))) }`,
	)
	if _, ok := funcResult(rep, "f"); ok {
		t.Error("f has no simplification; it should not appear in the report")
	}
	if rep.Applied != 0 {
		t.Errorf("applied = %d, want 0", rep.Applied)
	}
}

// TestOptimizeCommand drives the CLI (text + --json) and its error paths.
func TestOptimizeCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "o.mfl")
	src := "func scale(x) (r) { r = x * 8 }\nfunc main() { println(str(scale(2))) }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdOptimize([]string{path}); err != nil {
		t.Fatalf("optimize (text): %v", err)
	}
	if err := cmdOptimize([]string{"--json", path}); err != nil {
		t.Fatalf("optimize --json: %v", err)
	}
	if err := cmdOptimize(nil); err == nil {
		t.Fatal("expected a usage error with no source file")
	}
	if err := cmdOptimize([]string{filepath.Join(dir, "missing.mfl")}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	bad := filepath.Join(dir, "bad.mfl")
	os.WriteFile(bad, []byte("func main() { x := }\n"), 0o644)
	if err := cmdOptimize([]string{bad}); err == nil {
		t.Fatal("expected a parse/type error for malformed source")
	}
	// a program with no optimizable function still succeeds and prints the empty summary.
	none := filepath.Join(dir, "none.mfl")
	os.WriteFile(none, []byte("func main() { println(\"hi\") }\n"), 0o644)
	if err := cmdOptimize([]string{none}); err != nil {
		t.Fatalf("optimize (no rewrites): %v", err)
	}
	printOptReport(optReport{}) // empty-report renderer branch
}

// TestOptimizeRules unit-tests the individual rewrite rules and helpers.
func TestOptimizeRules(t *testing.T) {
	if k, ok := log2Pow2(8); !ok || k != 3 {
		t.Errorf("log2Pow2(8) = %d,%v", k, ok)
	}
	if _, ok := log2Pow2(6); ok {
		t.Error("6 is not a power of two")
	}
	if _, ok := log2Pow2(0); ok {
		t.Error("0 is not a power of two")
	}
	// constant folding across ops
	cf := ruleConstFold(&Binary{Op: "*", L: &IntLit{6}, R: &IntLit{7}})
	if l, ok := cf.(*IntLit); !ok || l.Val != 42 {
		t.Errorf("6*7 folded to %v", cf)
	}
	// division by zero is left intact
	if _, ok := ruleConstFold(&Binary{Op: "/", L: &IntLit{1}, R: &IntLit{0}}).(*Binary); !ok {
		t.Error("1/0 must not fold")
	}
	// identity: x - x -> 0 only for a pure ident
	z := ruleIdentity(&Binary{Op: "-", L: &Ident{"x"}, R: &Ident{"x"}})
	if l, ok := z.(*IntLit); !ok || l.Val != 0 {
		t.Errorf("x - x folded to %v", z)
	}
	// purity: a call is impure, so x*0 with a call operand is NOT reduced
	imp := ruleIdentity(&Binary{Op: "*", L: &Call{Callee: "f"}, R: &IntLit{0}})
	if _, ok := imp.(*Binary); !ok {
		t.Error("f()*0 must not reduce (f may have effects)")
	}
	if !isPure(&Binary{Op: "+", L: &Ident{"a"}, R: &IntLit{1}}) {
		t.Error("a+1 is pure")
	}
	if isPure(&Binary{Op: "/", L: &Ident{"a"}, R: &Ident{"b"}}) {
		t.Error("a/b can trap — not pure")
	}
}

// TestRenderRoundTrip: rendered optimized source reparses to an equivalent tree (spot-check
// via the renderer's completeness flag and a reparse).
func TestRenderRoundTrip(t *testing.T) {
	src, complete := renderFuncSrc(&FuncDecl{
		Name: "g", Params: []string{"a", "b"}, Returns: []string{"r"},
		Body: []Stmt{&AssignStmt{Name: "r", Op: "=", Val: &Binary{Op: "<<", L: &Ident{"a"}, R: &IntLit{2}}}},
	})
	if !complete {
		t.Fatal("renderer should be complete for this function")
	}
	if _, err := ParseProgram([]string{normalize(src)}); err != nil {
		t.Fatalf("rendered %q does not reparse: %v", src, err)
	}
	// a node the renderer can't handle flips complete=false
	_, complete = renderFuncSrc(&FuncDecl{
		Name: "h", Body: []Stmt{&GoStmt{Call: &Call{Callee: "f"}}},
	})
	if complete {
		t.Error("a GoStmt body should render as incomplete")
	}
}
