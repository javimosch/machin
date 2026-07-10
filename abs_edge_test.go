package main

import (
	"strings"
	"testing"
)

// TestAbsEdgeCases covers abs edge cases not tested in TestAbsAndRound:
// negative fractions, large magnitude values, and the identity property
// abs(-x) == abs(x).
func TestAbsEdgeCases(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("frac_neg=" + str(abs(-3.7)))
    println("frac_pos=" + str(abs(3.7)))
    println("large_neg=" + str(abs(-1000000.0)))
    println("large_pos=" + str(abs(1000000.0)))
    println("small_neg=" + str(abs(-0.05)))
    println("small_pos=" + str(abs(0.05)))
    println("identity=" + str(abs(-7.0) == abs(7.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"frac_neg=3.7",
		"frac_pos=3.7",
		"large_neg=1e+06",
		"large_pos=1e+06",
		"small_neg=0.05",
		"small_pos=0.05",
		"identity=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}