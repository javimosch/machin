package main

import (
	"strings"
	"testing"
)

func TestExamplePascal(t *testing.T) {
	out := runFile(t, "examples/complex/pascal.mfl")
	// Row 6 of Pascal's triangle.
	if !strings.Contains(out, "1  6  15  20  15  6  1") {
		t.Fatalf("pascal: got %q", out)
	}
}

func TestExampleEuler6(t *testing.T) {
	out := runFile(t, "examples/complex/euler6.mfl")
	if !strings.Contains(out, "= 25164150") {
		t.Fatalf("euler6: got %q", out)
	}
}
