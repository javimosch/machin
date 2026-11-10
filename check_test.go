package main

import (
	"testing"
)

// analyzeSource is the pure core of `machin check` — no I/O, no exit — so we assert on
// the structured verdict directly.

func TestCheckClean(t *testing.T) {
	r := analyzeSource("func add(a,b)(c){c=a+b}\nfunc main(){println(add(2,3))}\n", []string{"<t>"})
	if !r.OK || r.ErrorCount != 0 || len(r.Diagnostics) != 0 {
		t.Fatalf("clean program should have no diagnostics, got %+v", r)
	}
}

func TestCheckTypeMismatch(t *testing.T) {
	r := analyzeSource(`func main(){ y := "hi" > 3  println(y) }`+"\n", []string{"<t>"})
	if r.OK || r.ErrorCount != 1 {
		t.Fatalf("expected one error, got %+v", r)
	}
	d := r.Diagnostics[0]
	if d.Phase != "typecheck" || d.Code != "type-mismatch" {
		t.Fatalf("expected typecheck/type-mismatch, got phase=%q code=%q", d.Phase, d.Code)
	}
}

func TestCheckNilRejectedAtTypecheck(t *testing.T) {
	r := analyzeSource(`func main(){ x := nil  println(x) }`+"\n", []string{"<t>"})
	if r.OK || r.ErrorCount != 1 {
		t.Fatalf("expected one error for nil, got %+v", r)
	}
	d := r.Diagnostics[0]
	if d.Phase != "typecheck" {
		t.Fatalf("expected a typecheck error, got phase=%q", d.Phase)
	}
}

func TestCheckParseErrorDeclAndLine(t *testing.T) {
	src := "func good(a)(b){b=a+1}\n\nfunc broken(x)(y){\n    y = compute(x x)\n}\n"
	r := analyzeSource(src, []string{"<t>"})
	if r.OK || r.ErrorCount != 1 {
		t.Fatalf("expected one parse error, got %+v", r)
	}
	d := r.Diagnostics[0]
	if d.Phase != "parse" || d.Decl != "broken" || d.Line != 3 {
		t.Fatalf("expected parse error in broken@line3, got phase=%q decl=%q line=%d", d.Phase, d.Decl, d.Line)
	}
}

func TestCheckMultipleParseErrors(t *testing.T) {
	src := "func a(){ x = foo(1 2) }\nfunc b(){ y = bar(3 4) }\nfunc main(){}\n"
	r := analyzeSource(src, []string{"<t>"})
	if r.OK || r.ErrorCount != 2 {
		t.Fatalf("expected two parse errors, got count=%d %+v", r.ErrorCount, r)
	}
	if r.Diagnostics[0].Decl != "a" || r.Diagnostics[1].Decl != "b" {
		t.Fatalf("expected decls a,b, got %q,%q", r.Diagnostics[0].Decl, r.Diagnostics[1].Decl)
	}
}

func TestCheckUnbalancedBraces(t *testing.T) {
	r := analyzeSource("func broken(){\n    if x {\n", []string{"<t>"})
	if r.OK || r.Diagnostics[0].Code != "parse-unbalanced-braces" {
		t.Fatalf("expected parse-unbalanced-braces, got %+v", r)
	}
}

func TestCheckNoMain(t *testing.T) {
	r := analyzeSource("func helper(a)(b){b=a}\n", []string{"<t>"})
	if r.OK || r.Diagnostics[0].Code != "no-main" {
		t.Fatalf("expected no-main, got %+v", r)
	}
}

// #88: two functions with the same name used to be silently accepted (the
// second definition overwrote the first in the checker's lookup map, no
// diagnostic) — a real footgun for framework composition, where an app
// function name colliding with a framework one silently wins or loses
// depending on declaration order. Must now be a typecheck error.
func TestCheckDuplicateFunction(t *testing.T) {
	src := `func greet(){return "FIRST"}
func greet(){return "SECOND"}
func main(){println(greet())}
`
	r := analyzeSource(src, []string{"<t>"})
	if r.OK || r.ErrorCount != 1 {
		t.Fatalf("expected one error, got %+v", r)
	}
	d := r.Diagnostics[0]
	if d.Phase != "typecheck" || d.Code != "duplicate-function" {
		t.Fatalf("expected typecheck/duplicate-function, got phase=%q code=%q", d.Phase, d.Code)
	}
}

// #645: `check --all` makes every declaration a typecheck root, so a library
// function nothing calls yet is still checked — the case where completeness
// matters more than dead-code pruning.

func TestCheckAllUnreachableFunc(t *testing.T) {
	// the issue repro: an undefined call inside a function main never reaches.
	// Default check passes (reachability is deliberate); --all must flag it.
	src := "func never_called(x) (out) {\n  a, b := totally_undefined(x)\n  out = str(a) + b\n}\nfunc main() { println(\"hi\") }\n"
	if r := analyzeSource(src, []string{"<t>"}); !r.OK {
		t.Fatalf("default check should stay clean (unreachable), got %+v", r)
	}
	r := analyzeSourceSpan(src, []string{"<t>"}, nil, true)
	if r.OK || r.ErrorCount == 0 {
		t.Fatalf("--all should flag the unreachable function's error, got %+v", r)
	}
	if r.Diagnostics[0].Phase != "typecheck" {
		t.Fatalf("expected typecheck error, got %+v", r.Diagnostics[0])
	}
}

func TestCheckAllNoMainLibrary(t *testing.T) {
	// a library file has no main and no export — default check reports no-main,
	// but --all checks the declarations themselves (and finds their errors).
	src := "func helper(x) (out) {\n  out = str(x) + \"!\"\n}\nfunc unused_bad(y) {\n  z := missing_fn(y)\n  println(z)\n}\n"
	r := analyzeSourceSpan(src, []string{"<t>"}, nil, true)
	if r.OK || r.ErrorCount == 0 {
		t.Fatalf("--all should flag missing_fn in the uncalled function, got %+v", r)
	}
}

func TestCheckAllClean(t *testing.T) {
	// --all on a clean program (including an unused-but-valid function) stays clean.
	src := "func helper(x) (out) { out = str(x) + \"!\" }\nfunc unused_ok(y) { println(helper(y)) }\nfunc main() { println(helper(1)) }\n"
	r := analyzeSourceSpan(src, []string{"<t>"}, nil, true)
	if !r.OK || r.ErrorCount != 0 {
		t.Fatalf("--all should stay clean, got %+v", r)
	}
}

// #645 follow-on: root instances have no caller to pin parameter types, so
// field/index uses on them were unresolvable ("cannot infer struct type",
// "cannot index"). The checker now completes them: a unique struct field name
// identifies the struct, and an unbound index base commits to slice (or map
// for a non-int key). This also un-breaks `export func` with struct params.
func TestCheckAllInfersParamTypes(t *testing.T) {
	src := `type Point struct { x int  y int }
func dist2(p) (out) { out = p.x*p.x + p.y*p.y }
func idx_sum(xs, n) (out) { out = 0  i := 0  while i < n { out = out + xs[i]  i = i + 1 } }
func lookup(m, k) (out) { out = m[k] }
func main() { p := Point{3,4}  println(dist2(p)) }
`
	r := analyzeSourceSpan(src, []string{"<t>"}, nil, true)
	if !r.OK || r.ErrorCount != 0 {
		t.Fatalf("--all should infer param types for field/index uses, got %+v", r)
	}
	// dist2 is called (call-site instance); idx_sum/lookup are uncalled, so
	// --all roots them with free params — their xs[i]/m[k] uses must infer.
}

func TestCheckAllAmbiguousFieldStillFails(t *testing.T) {
	// two structs share field `x` — for an UNCALLED function the access is
	// genuinely ambiguous and must still error rather than guess.
	src := `type A struct { x int }
type B struct { x int  y int }
func getx(v) (out) { out = v.x }
func main() { println("hi") }
`
	r := analyzeSourceSpan(src, []string{"<t>"}, nil, true)
	if r.OK || r.ErrorCount == 0 {
		t.Fatalf("ambiguous field access should still fail under --all, got %+v", r)
	}
}

func TestCheckAllSkipsInstantiatedFuncs(t *testing.T) {
	// a function already instantiated through a call site is NOT re-rooted:
	// its ambiguous-looking field access is pinned by the caller, so --all
	// must not invent a second, free-param instance that would false-positive.
	src := `type A struct { x int }
type B struct { x int  y int }
func getx(v) (out) { out = v.x }
func main() { println(getx(A{1})) }
`
	r := analyzeSourceSpan(src, []string{"<t>"}, nil, true)
	if !r.OK || r.ErrorCount != 0 {
		t.Fatalf("called function should not be re-rooted under --all, got %+v", r)
	}
}
