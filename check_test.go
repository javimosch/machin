package main

import "testing"

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
