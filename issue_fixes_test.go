package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// checkErr parses readable function sources (through the real base64 path) and
// runs the type checker, returning any compile-time error. It deliberately
// stops before codegen so we can assert on clean MFL type errors.
func checkErr(t *testing.T, funcs ...string) error {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		enc := base64.StdEncoding.EncodeToString([]byte(normalize(f)))
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("b64: %v", err)
		}
		fn, err := ParseFunc(string(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	_, err := Check(fns)
	return err
}

// Issue #1: string ==/!= must compare values, not C pointers.
func TestIssue1StringEquality(t *testing.T) {
	got := runNative(t, `func main() { a := "ab" b := "a" + "b" if a == b { println("equal") } else { println("not equal") } }`)
	if got != "equal\n" {
		t.Fatalf("string == should compare by value, got %q", got)
	}
	got = runNative(t, `func main() { if "ab" != "ac" { println("differ") } else { println("same") } }`)
	if got != "differ\n" {
		t.Fatalf("string != should compare by value, got %q", got)
	}
}

// Issue #2: % on floats must be a clean compile-time type error, not a raw cc error.
func TestIssue2FloatModulo(t *testing.T) {
	err := checkErr(t, `func main() { x := 3.5 y := 2.0 println(x % y) }`)
	if err == nil {
		t.Fatal("float % should be a compile-time type error")
	}
	// integer modulo must still type-check and run.
	if got := runNative(t, `func main() { println(7 % 3) }`); got != "1\n" {
		t.Fatalf("int %% regressed, got %q", got)
	}
}

// Issue #3: len() on a non-string/non-slice must be a compile-time type error.
func TestIssue3LenTypeCheck(t *testing.T) {
	err := checkErr(t, `func main() { println(len(5)) }`)
	if err == nil {
		t.Fatal("len(int) should be a compile-time type error")
	}
	if !strings.Contains(err.Error(), "string or slice") {
		t.Fatalf("unexpected len error: %v", err)
	}
	// len on strings and slices must still work.
	if got := runNative(t, `func main() { s := []int{1, 2, 3} println(len("hello"), len(s)) }`); got != "5 3\n" {
		t.Fatalf("len on string/slice regressed, got %q", got)
	}
}
