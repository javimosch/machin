package main

import (
	"strings"
	"testing"
)

// runMFLFile loads an actual .mfl example file (the base64 machine-first path),
// compiles it to native via cc, runs it, and returns stdout. This guards the
// shipped example programs against regressions in the lexer/parser/codegen.
func runMFLFile(t *testing.T, path string) string {
	t.Helper()
	funcs, err := loadMFL(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	out, err := RunCaptured(funcs)
	if err != nil {
		t.Fatalf("run %s: %v", path, err)
	}
	return out
}

func TestExampleFibonacci(t *testing.T) {
	got := runMFLFile(t, "examples/complex/fibonacci.mfl")
	for _, want := range []string{"fib 0 = 0", "fib 7 = 13", "fib 14 = 377"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fibonacci output missing %q; got:\n%s", want, got)
		}
	}
}

func TestExampleReverseDigits(t *testing.T) {
	got := runMFLFile(t, "examples/complex/reverse_digits.mfl")
	for _, want := range []string{"reverse 12345 = 54321", "reverse 1000 = 1", "reverse 7 = 7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reverse_digits output missing %q; got:\n%s", want, got)
		}
	}
}

func TestExampleTriangular(t *testing.T) {
	got := runMFLFile(t, "examples/complex/triangular.mfl")
	for _, want := range []string{"T 1 = 1", "T 5 = 15", "T 10 = 55"} {
		if !strings.Contains(got, want) {
			t.Fatalf("triangular output missing %q; got:\n%s", want, got)
		}
	}
}

func TestExampleDigitSum(t *testing.T) {
	got := runMFLFile(t, "examples/complex/digit_sum.mfl")
	for _, want := range []string{"digit_sum 1234 = 10", "digit_sum 99999 = 45", "digit_sum 0 = 0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("digit_sum output missing %q; got:\n%s", want, got)
		}
	}
}
