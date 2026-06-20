package main

import (
	"strings"
	"testing"
)

func TestExampleHarmonic(t *testing.T) {
	out := runFile(t, "examples/complex/harmonic.mfl")
	if !strings.Contains(out, "H(10) = 2.92897") {
		t.Fatalf("harmonic: got %q", out)
	}
}
