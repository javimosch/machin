package main

import (
	"strings"
	"testing"
)

// compileErr parses the given function sources and runs them through the full
// type-check + codegen pipeline (CompileToC), returning any error. It is used
// by the correctness tests that expect a clean MFL compile/type error rather
// than leaked C-compiler output or undefined runtime behavior.
func compileErr(t *testing.T, funcs ...string) error {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		fn, err := ParseFunc(normalize(f))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	_, err := CompileToC(fns)
	return err
}

// Fixes #1: string ==/!= must compare contents, not the underlying C pointers.
// b is built by concatenation so it is a distinct allocation from a; a pointer
// comparison would report them unequal.
func TestStringEqualityByValue(t *testing.T) {
	got := runNative(t,
		`func main() { a := "ab" b := "a" + "b" if a == b { println("equal") } else { println("not equal") } }`)
	if got != "equal\n" {
		t.Fatalf("string == should be by value, got %q", got)
	}
}

func TestStringInequalityByValue(t *testing.T) {
	got := runNative(t,
		`func main() { a := "ab" b := "a" + "c" if a != b { println("diff") } else { println("same") } }`)
	if got != "diff\n" {
		t.Fatalf("string != should be by value, got %q", got)
	}
}

// Fixes #2: % on floats must be a clean MFL type error, not leaked cc output.
func TestFloatModuloIsTypeError(t *testing.T) {
	err := compileErr(t, `func main() { x := 1.5 println(x % 2.0) }`)
	if err == nil {
		t.Fatal("float %% should be a compile-time type error")
	}
	if strings.Contains(err.Error(), "cc failed") {
		t.Fatalf("float %% leaked raw cc output instead of a clean MFL error: %v", err)
	}
}

// A guard so the fix does not over-constrain: integer modulo must still work.
func TestIntModuloStillWorks(t *testing.T) {
	if got := runNative(t, `func main() { println(17 % 5) }`); got != "2\n" {
		t.Fatalf("int %% regressed, got %q", got)
	}
}

// Fixes #3: len() on an int must be rejected instead of compiling to strlen()
// on a non-pointer value.
func TestLenOnIntIsTypeError(t *testing.T) {
	err := compileErr(t, `func main() { println(len(42)) }`)
	if err == nil {
		t.Fatal("len(int) should be a compile-time type error")
	}
	if strings.Contains(err.Error(), "cc failed") {
		t.Fatalf("len(int) leaked raw cc output instead of a clean MFL error: %v", err)
	}
}

// Guards: len() must still work on its valid argument kinds.
func TestLenOnStringStillWorks(t *testing.T) {
	if got := runNative(t, `func main() { println(len("hello")) }`); got != "5\n" {
		t.Fatalf("len(string) regressed, got %q", got)
	}
}

func TestLenOnSliceStillWorks(t *testing.T) {
	if got := runNative(t, `func main() { xs := []int{1, 2, 3} println(len(xs)) }`); got != "3\n" {
		t.Fatalf("len(slice) regressed, got %q", got)
	}
}
