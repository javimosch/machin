package main

import (
	"strings"
	"testing"
)

// runFile compiles and runs an .mfl example end to end, returning its stdout.
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

func TestExampleFactorial(t *testing.T) {
	out := runFile(t, "examples/complex/factorial.mfl")
	if !strings.Contains(out, "10 ! = 3628800") {
		t.Fatalf("factorial: got %q", out)
	}
}

func TestExampleAbundant(t *testing.T) {
	out := runFile(t, "examples/complex/abundant.mfl")
	if !strings.Contains(out, "12 is abundant") || !strings.Contains(out, "up to 100: 22") {
		t.Fatalf("abundant: got %q", out)
	}
}

func TestExampleMean(t *testing.T) {
	out := runFile(t, "examples/complex/mean.mfl")
	if !strings.Contains(out, "mean of 1..10 = 5.5") {
		t.Fatalf("mean: got %q", out)
	}
}
