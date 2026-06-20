package main

import (
	"encoding/base64"
	"testing"
)

// runNative parses readable function sources, round-trips them through base64
// (the real machine-first path), compiles to native via cc, runs, and returns
// stdout.
func runNative(t *testing.T, funcs ...string) string {
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
	out, err := RunCaptured(fns)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

func TestArithmetic(t *testing.T) {
	if got := runNative(t, `func main() { println(2 + 3 * 4) }`); got != "14\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRecursionAndLoop(t *testing.T) {
	got := runNative(t,
		`func fib(n) { if n < 2 { return n } return fib(n-1) + fib(n-2) }`,
		`func main() { println(fib(10)) }`)
	if got != "55\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringsAndBools(t *testing.T) {
	if got := runNative(t, `func main() { println("a" + "b", 1 == 1, !false) }`); got != "ab true true\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFloatInference(t *testing.T) {
	// k starts int but unifies to float via 2.0 * k; division is float.
	got := runNative(t, `func main() { k := 0 println(7 / 2, 2.0 * k + 7.0 / 2.0) }`)
	if got != "3 3.5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringBuilding(t *testing.T) {
	got := runNative(t,
		`func bin(n) { if n < 2 { return str(n) } return bin(n/2) + str(n%2) }`,
		`func main() { println(bin(10)) }`)
	if got != "1010\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSlices(t *testing.T) {
	got := runNative(t,
		`func main() { xs := []int{1, 2, 3} xs = append(xs, 4) xs[0] = 10 s := 0 i := 0 for i < len(xs) { s = s + xs[i] i = i + 1 } println(s, len(xs)) }`)
	if got != "19 4\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSliceParamInference(t *testing.T) {
	got := runNative(t,
		`func first(xs) { return xs[0] }`,
		`func main() { println(first([]string{"a", "b"})) }`)
	if got != "a\n" {
		t.Fatalf("got %q", got)
	}
}

func TestGoroutine(t *testing.T) {
	got := runNative(t,
		`func w() { println("hi") }`,
		`func main() { go w() sleep(50) println("done") }`)
	if got != "hi\ndone\n" {
		t.Fatalf("got %q", got)
	}
}

// mustNotCompile parses the given function sources and asserts that type
// checking rejects them — used for the compile-time type-error regressions.
func mustNotCompile(t *testing.T, funcs ...string) error {
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
	if err == nil {
		t.Fatal("expected a compile-time type error, got none")
	}
	return err
}

// Issue #1: string ==/!= must compare contents, not C pointer identity, so
// two equal-but-distinct strings compare equal.
func TestStringEquality(t *testing.T) {
	got := runNative(t, `func main() { a := "ab" b := "a" + "b" if a == b { println("equal") } else { println("not equal") } }`)
	if got != "equal\n" {
		t.Fatalf("got %q", got)
	}
}

// Issue #2: % on floats is a compile-time MFL type error, not leaked cc output.
func TestModuloFloatRejected(t *testing.T) {
	mustNotCompile(t, `func main() { x := 5.0 y := 2.0 println(x % y) }`)
}

// Issue #3: len() on a non-string/non-slice is a compile-time type error,
// not a strlen() on a non-pointer at runtime.
func TestLenOnIntRejected(t *testing.T) {
	mustNotCompile(t, `func main() { n := 42 println(len(n)) }`)
}

func TestTypeMismatch(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { x := 1 x = "s" }`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check([]*FuncDecl{fn}); err == nil {
		t.Fatal("expected type mismatch error")
	}
}

func TestSplitFunctions(t *testing.T) {
	fns, err := splitFunctions("func a() { return 1 }\n\nfunc b() { return 2 }\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 2 {
		t.Fatalf("expected 2 funcs, got %d", len(fns))
	}
}
