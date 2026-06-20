package main

import (
	"strings"
	"testing"
)

// mustCheck parses readable function sources and runs the type checker,
// returning the error (if any). It is the type-check-only path used to assert
// that bad programs are rejected with a clean MFL error, never a leaked cc error.
func mustCheck(t *testing.T, funcs ...string) error {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		fn, err := ParseFunc(normalize(f))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	_, err := Check(fns)
	return err
}

// Issue #1: string == / != must compare contents, not pointers. Distinct
// allocations holding equal text must compare equal.
func TestStringEqualityByValue(t *testing.T) {
	got := runNative(t,
		`func main() { a := "ab" b := "a" + "b" if a == b { println("equal") } else { println("not equal") } }`)
	if got != "equal\n" {
		t.Fatalf("string == compared by pointer, got %q", got)
	}
	got = runNative(t, `func main() { if "ab" != "ac" { println("differ") } }`)
	if got != "differ\n" {
		t.Fatalf("string != broken, got %q", got)
	}
}

// Issue #2: % on floats is rejected at type-check (C's % is integer-only), so
// the error is a clean MFL type error rather than a leaked cc error.
func TestFloatModuloRejected(t *testing.T) {
	err := mustCheck(t, `func main() { x := 3.5 y := 2.0 println(x % y) }`)
	if err == nil {
		t.Fatal("float % should be a type error")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected MFL type error, got %v", err)
	}
}

// Integer % must still type-check and run.
func TestIntModuloAllowed(t *testing.T) {
	if got := runNative(t, `func main() { println(17 % 5) }`); got != "2\n" {
		t.Fatalf("int %% broken, got %q", got)
	}
}

// Issue #3: len() on a non-string/non-slice argument is rejected at type-check
// instead of compiling to strlen() on a non-pointer (undefined behavior).
func TestLenOnIntRejected(t *testing.T) {
	err := mustCheck(t, `func main() { println(len(5)) }`)
	if err == nil {
		t.Fatal("len(int) should be a type error")
	}
	if !strings.Contains(err.Error(), "string or slice") {
		t.Fatalf("expected len type error, got %v", err)
	}
}

// len() on strings and slices must still work.
func TestLenOnStringAndSlice(t *testing.T) {
	got := runNative(t, `func main() { s := "hello" xs := []int{1, 2, 3} println(len(s), len(xs)) }`)
	if got != "5 3\n" {
		t.Fatalf("len on string/slice broken, got %q", got)
	}
}
