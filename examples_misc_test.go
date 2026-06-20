package main

import "testing"

// runExample loads a real .mfl file (the base64 machine-first form), compiles it
// to native via cc, runs it, and returns stdout — guarding the shipped examples
// against regressions in the parser/codegen.
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

func TestHammingDistanceExample(t *testing.T) {
	got := runExample(t, "examples/complex/hamming_distance.mfl")
	want := "hamming(9,14) = 3\nhamming(0,255) = 8\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestESeriesExample(t *testing.T) {
	got := runExample(t, "examples/complex/e_series.mfl")
	want := "e approx = 2.71828\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDigitProductExample(t *testing.T) {
	got := runExample(t, "examples/complex/digit_product.mfl")
	want := "digit_product(234) = 24\ndigit_product(999) = 729\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
