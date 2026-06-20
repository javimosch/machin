package main

import "testing"

// runFile loads a real .mfl file (the machine-first path: base64 lines decoded
// and parsed), compiles to native, runs, and returns stdout.
func runFile(t *testing.T, path string) string {
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

func TestPrimeFactorsExample(t *testing.T) {
	// 360 = 2^3 * 3^2 * 5.
	want := "prime factorization of 360\n2\n2\n2\n3\n3\n5\n"
	if got := runFile(t, "examples/complex/prime_factors.mfl"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLucasExample(t *testing.T) {
	// Lucas numbers L(0..11): 2 1 3 4 7 11 18 29 47 76 123 199.
	want := "2\n1\n3\n4\n7\n11\n18\n29\n47\n76\n123\n199\n"
	if got := runFile(t, "examples/complex/lucas.mfl"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCatalanExample(t *testing.T) {
	// Catalan numbers C(0..9): 1 1 2 5 14 42 132 429 1430 4862.
	want := "1\n1\n2\n5\n14\n42\n132\n429\n1430\n4862\n"
	if got := runFile(t, "examples/complex/catalan.mfl"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
