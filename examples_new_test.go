package main

import "testing"

// Each case loads a real .mfl example from disk (the machine-first base64 path),
// compiles it to native via cc, runs it, and asserts deterministic stdout. This
// guards the examples against regressions in the parser, type checker, or codegen.
func runExample(t *testing.T, path string) string {
	t.Helper()
	fns, err := loadMFL(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	out, err := RunCaptured(fns)
	if err != nil {
		t.Fatalf("run %s: %v", path, err)
	}
	return out
}

func TestExamplePalindrome(t *testing.T) {
	if got := runExample(t, "examples/complex/palindrome.mfl"); got != "two-digit palindromes: 9\n" {
		t.Fatalf("got %q", got)
	}
}

func TestExampleBinomial(t *testing.T) {
	want := "C(10,3) = 120 C(6,2) = 15 C(20,10) = 184756\n"
	if got := runExample(t, "examples/complex/binomial.mfl"); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestExampleSumSquares(t *testing.T) {
	want := "sum of squares = 385 square of sum = 3025 diff = 2640\n"
	if got := runExample(t, "examples/complex/sum_squares.mfl"); got != want {
		t.Fatalf("got %q", got)
	}
}
