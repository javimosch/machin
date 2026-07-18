package main

import (
	"os"
	"path/filepath"
	"testing"
)

// synthTarget parses + checks a program and returns (target func, funcs, structs) for synthesis.
func synthTarget(t *testing.T, name string, decls ...string) (*FuncDecl, map[string]*FuncDecl, map[string]*TypeDecl) {
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
	funcs := map[string]*FuncDecl{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	for _, inst := range c.reps {
		if fn := c.instFn[inst]; fn != nil && fn.Name == name {
			return fn, funcs, c.structs
		}
	}
	t.Fatalf("target %q not instantiated", name)
	return nil, nil, nil
}

// TestSynthFindsLoopElimination: a constant-count loop is discovered to equal a cheap
// straight-line form, eliminating the loop, and the winner survives wide-domain verification.
func TestSynthFindsLoopElimination(t *testing.T) {
	target, funcs, structs := synthTarget(t, "triple",
		`func triple(a) (r) { r = 0  i := 0  for i < 3 { r = r + a  i = i + 1 } }`,
		`func main() { println(str(triple(4))) }`,
	)
	res := synthesize(target, target.Params, funcs, structs)
	if !res.Found {
		t.Fatalf("expected to discover a closed form for triple; note=%q explored=%d", res.Note, res.Explored)
	}
	if res.CostAfter >= res.CostBefore {
		t.Errorf("found form must be cheaper: %d -> %d", res.CostBefore, res.CostAfter)
	}
	if res.WidePoints == 0 {
		t.Error("winner should be wide-verified")
	}
	// the discovered expression is a genuine equivalent over the sample domain
	if res.Expr == "" {
		t.Error("expected a rendered expression")
	}
}

// TestSynthMultiParam: a two-parameter redundant expression is factored to a cheaper equivalent.
func TestSynthMultiParam(t *testing.T) {
	target, funcs, structs := synthTarget(t, "redun",
		`func redun(a, b) (r) { r = a * b + a * b }`,
		`func main() { println(str(redun(2, 3))) }`,
	)
	res := synthesize(target, target.Params, funcs, structs)
	if !res.Found || res.CostAfter >= res.CostBefore {
		t.Fatalf("expected a cheaper equivalent for redun; got %+v", res)
	}
}

// TestSynthRejectsOverfit: sum-to-n has NO simple closed form that agrees on negative inputs
// (the loop returns 0 for n<=0), so the dense sample domain + wide re-check refuse a rewrite.
// This is the guard against synthesizing a rewrite that only matches a sparse sample.
func TestSynthRejectsOverfit(t *testing.T) {
	target, funcs, structs := synthTarget(t, "sumn",
		`func sumn(n) (r) { r = 0  i := 1  for i <= n { r = r + i  i = i + 1 } }`,
		`func main() { println(str(sumn(4))) }`,
	)
	res := synthesize(target, target.Params, funcs, structs)
	if res.Found {
		t.Errorf("sum-to-n has no closed form valid on negatives; must not 'find' %q", res.Expr)
	}
}

// TestSynthAlreadyMinimal: a function already at minimum cost yields no cheaper equivalent.
func TestSynthAlreadyMinimal(t *testing.T) {
	target, funcs, structs := synthTarget(t, "inc",
		`func inc(a) (r) { r = a + 1 }`,
		`func main() { println(str(inc(4))) }`,
	)
	res := synthesize(target, target.Params, funcs, structs)
	if res.Found {
		t.Errorf("a+1 is already minimal; must not 'find' %q", res.Expr)
	}
}

// TestSynthUnsupported: zero params, too many params, and non-int returns are declined with a note.
func TestSynthUnsupported(t *testing.T) {
	// zero params
	target, funcs, structs := synthTarget(t, "z",
		`func z() (r) { r = 5 }`, `func main() { println(str(z())) }`)
	if r := synthesize(target, target.Params, funcs, structs); r.Found || r.Note == "" {
		t.Error("zero-param function should be declined")
	}
	// three params
	target, funcs, structs = synthTarget(t, "three",
		`func three(a, b, cc) (r) { r = a + b + cc }`, `func main() { println(str(three(1, 2, 3))) }`)
	if r := synthesize(target, target.Params, funcs, structs); r.Found || r.Note == "" {
		t.Error("3-param function should be declined (search space)")
	}
	// non-int return (string)
	target, funcs, structs = synthTarget(t, "s",
		`func s(a) (r) { r = "x" }`, `func main() { println(s(1)) }`)
	if r := synthesize(target, target.Params, funcs, structs); r.Found || r.Note == "" {
		t.Error("non-int function should be declined")
	}
}

// TestSynthEvalInt unit-tests the fast integer evaluator including trap paths.
func TestSynthEvalInt(t *testing.T) {
	pidx := map[string]int{"a": 0, "b": 1}
	env := []int64{6, 3}
	cases := []struct {
		e    Expr
		want int64
		ok   bool
	}{
		{&IntLit{5}, 5, true},
		{&Ident{"a"}, 6, true},
		{&Unary{Op: "-", X: &Ident{"a"}}, -6, true},
		{&Binary{Op: "+", L: &Ident{"a"}, R: &Ident{"b"}}, 9, true},
		{&Binary{Op: "-", L: &Ident{"a"}, R: &Ident{"b"}}, 3, true},
		{&Binary{Op: "*", L: &Ident{"a"}, R: &Ident{"b"}}, 18, true},
		{&Binary{Op: "/", L: &Ident{"a"}, R: &Ident{"b"}}, 2, true},
		{&Binary{Op: "%", L: &Ident{"a"}, R: &Ident{"b"}}, 0, true},
		{&Binary{Op: "/", L: &Ident{"a"}, R: &IntLit{0}}, 0, false}, // div by zero traps
		{&Binary{Op: "%", L: &Ident{"a"}, R: &IntLit{0}}, 0, false},
	}
	for i, c := range cases {
		got, ok := evalInt(c.e, env, pidx)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("case %d: evalInt = (%d,%v), want (%d,%v)", i, got, ok, c.want, c.ok)
		}
	}
}

// TestSynthHelpers covers iota64, tupleCount, exprToStr, and mustParseExpr.
func TestSynthHelpers(t *testing.T) {
	if got := iota64(-2, 2); len(got) != 5 || got[0] != -2 || got[4] != 2 {
		t.Errorf("iota64(-2,2) = %v", got)
	}
	if n := tupleCount([][]int64{{1, 2, 3}, {1, 2}}); n != 6 {
		t.Errorf("tupleCount = %d, want 6", n)
	}
	if s := exprToStr(&Binary{Op: "*", L: &Ident{"a"}, R: &IntLit{2}}); s != "a * 2" {
		t.Errorf("exprToStr = %q", s)
	}
	e := mustParseExpr("a + a")
	if _, ok := e.(*Binary); !ok {
		t.Errorf("mustParseExpr returned %T", e)
	}
	if _, ok := mustParseExpr("@@@ not an expr").(*IntLit); !ok {
		t.Error("a bad expr should fall back to IntLit 0")
	}
}

// TestSuperoptCommand drives the CLI end to end plus its error paths.
func TestSuperoptCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.mfl")
	src := "func triple(a) (r) { r = 0  i := 0  for i < 3 { r = r + a  i = i + 1 } }\nfunc main() { println(str(triple(4))) }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdSuperopt([]string{"triple", path}); err != nil {
		t.Fatalf("superopt (text): %v", err)
	}
	if err := cmdSuperopt([]string{"triple", path, "--json"}); err != nil {
		t.Fatalf("superopt --json: %v", err)
	}
	if err := cmdSuperopt([]string{path}); err == nil {
		t.Fatal("expected usage error with no function name")
	}
	if err := cmdSuperopt([]string{"triple"}); err == nil {
		t.Fatal("expected usage error with no source file")
	}
	if err := cmdSuperopt([]string{"ghost", path}); err == nil {
		t.Fatal("expected an error for an unknown function")
	}
	if err := cmdSuperopt([]string{"triple", filepath.Join(dir, "missing.mfl")}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	// unsupported target still succeeds (prints a note, no error)
	none := filepath.Join(dir, "n.mfl")
	os.WriteFile(none, []byte("func z() (r) { r = 5 }\nfunc main() { println(str(z())) }\n"), 0o644)
	if err := cmdSuperopt([]string{"z", none}); err != nil {
		t.Fatalf("superopt on an unsupported target should not error: %v", err)
	}
	printSynthResult(synthResult{Fn: "x"}) // not-found renderer branch
}
