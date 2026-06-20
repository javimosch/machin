package main

import (
	"strings"
	"testing"
)

// runMFLFile loads a committed .mfl program through the real machine-first
// path (base64 decode -> parse) and runs it natively, returning stdout.
func runMFLFile(t *testing.T, path string) string {
	t.Helper()
	fns, err := loadMFL(path)
	if err != nil {
		t.Fatalf("loadMFL %s: %v", path, err)
	}
	out, err := RunCaptured(fns)
	if err != nil {
		t.Fatalf("run %s: %v", path, err)
	}
	return out
}

// TestArmstrongExample guards examples/complex/armstrong.mfl: the narcissistic
// numbers below 1000 are a fixed, well-known set.
func TestArmstrongExample(t *testing.T) {
	out := runMFLFile(t, "examples/complex/armstrong.mfl")
	for _, want := range []string{"153", "370", "371", "407"} {
		if !strings.Contains(out, "\n"+want+"\n") {
			t.Fatalf("armstrong output missing %q:\n%s", want, out)
		}
	}
	// 100 is not narcissistic (1 != 1^3); make sure we are not over-reporting.
	if strings.Contains(out, "\n100\n") {
		t.Fatalf("armstrong wrongly reported 100:\n%s", out)
	}
}

// TestJosephusExample guards examples/complex/josephus.mfl against regression
// in the survivor recurrence. n=41, k=3 is the textbook value (31).
func TestJosephusExample(t *testing.T) {
	out := runMFLFile(t, "examples/complex/josephus.mfl")
	if !strings.Contains(out, "n = 41, k = 3 -> 31") {
		t.Fatalf("josephus output wrong:\n%s", out)
	}
}

// TestBenchExampleCompiles ensures the benchmark program stays buildable and
// computes fib(40) correctly — the figure docs/BENCHMARKS.md depends on.
func TestBenchExampleCompiles(t *testing.T) {
	out := runMFLFile(t, "examples/bench/fib.mfl")
	if strings.TrimSpace(out) != "102334155" {
		t.Fatalf("fib(40) = %q, want 102334155", strings.TrimSpace(out))
	}
}
