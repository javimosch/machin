package main

import "testing"

// checkErr parses one function source and runs the type checker, returning the
// (possibly nil) error. Used by the expect-error cases below.
func checkErr(t *testing.T, src string) error {
	t.Helper()
	fn, err := ParseFunc(normalize(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check([]*FuncDecl{fn})
	return err
}

// Issue #1: string ==/!= must compare contents, not pointers.
func TestStringEquality(t *testing.T) {
	got := runNative(t,
		`func main() { a := "ab" b := "a" + "b" if a == b { println("equal") } else { println("not equal") } println(a != b, a != "x") }`)
	if got != "equal\nfalse true\n" {
		t.Fatalf("got %q", got)
	}
}

// Issue #2: % on floats is a clean MFL type error, not a raw cc failure.
func TestFloatModuloRejected(t *testing.T) {
	if err := checkErr(t, `func main() { x := 3.5 println(x % 2.0) }`); err == nil {
		t.Fatal("expected type error for float modulo")
	}
	// integer modulo still works.
	if got := runNative(t, `func main() { println(17 % 5) }`); got != "2\n" {
		t.Fatalf("got %q", got)
	}
}

// Issue #3: len() on a non-string/non-slice is a compile-time error.
func TestLenOnIntRejected(t *testing.T) {
	if err := checkErr(t, `func main() { n := 5 println(len(n)) }`); err == nil {
		t.Fatal("expected type error for len(int)")
	}
	// len on string and slice still works.
	if got := runNative(t, `func main() { println(len("abc"), len([]int{1, 2})) }`); got != "3 2\n" {
		t.Fatalf("got %q", got)
	}
}
