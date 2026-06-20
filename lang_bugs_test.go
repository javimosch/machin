package main

import (
	"strings"
	"testing"
)

// parseFuncs turns readable function sources into FuncDecls via the canonical
// normalize -> parse path (no base64 round-trip needed for type-check checks).
func parseFuncs(t *testing.T, funcs ...string) []*FuncDecl {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		fn, err := ParseFunc(normalize(f))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	return fns
}

// Issue #1: string ==/!= must compare contents, not pointers. "ab" and
// "a"+"b" are equal strings stored at different addresses.
func TestStringEquality(t *testing.T) {
	got := runNative(t,
		`func main() { a := "ab" b := "a" + "b" if a == b { println("equal") } else { println("not equal") } }`)
	if got != "equal\n" {
		t.Fatalf("== got %q want %q", got, "equal\n")
	}
	got = runNative(t,
		`func main() { a := "ab" b := "ac" if a != b { println("differ") } else { println("same") } }`)
	if got != "differ\n" {
		t.Fatalf("!= got %q want %q", got, "differ\n")
	}
}

// Issue #2: % on floats must be a clean MFL type error at check time, not a
// raw cc failure that leaks generated C.
func TestFloatModuloTypeError(t *testing.T) {
	_, err := Check(parseFuncs(t, `func main() { x := 3.0 y := 2.0 println(x % y) }`))
	if err == nil {
		t.Fatal("expected a type error for float % float, got nil")
	}
	if strings.Contains(err.Error(), "cc") || strings.Contains(err.Error(), "#include") {
		t.Fatalf("error leaked raw C/cc output: %v", err)
	}
	// Integer modulo must still type-check.
	if _, err := Check(parseFuncs(t, `func main() { println(7 % 3) }`)); err != nil {
		t.Fatalf("int %% int should type-check, got %v", err)
	}
}

// Issue #3: len() on a non-string/non-slice must be a clean type error rather
// than compiling to strlen() on a non-pointer.
func TestLenArgTypeError(t *testing.T) {
	_, err := Check(parseFuncs(t, `func main() { println(len(42)) }`))
	if err == nil {
		t.Fatal("expected a type error for len(int), got nil")
	}
	if strings.Contains(err.Error(), "cc") || strings.Contains(err.Error(), "#include") {
		t.Fatalf("error leaked raw C/cc output: %v", err)
	}
	// len on string and slice must still work.
	got := runNative(t, `func main() { s := "hello" xs := []int{1, 2, 3} println(len(s), len(xs)) }`)
	if got != "5 3\n" {
		t.Fatalf("len(string), len(slice) got %q want %q", got, "5 3\n")
	}
}
