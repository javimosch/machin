package main

import (
	"strings"
	"testing"
)

// abs and round are simple math functions that had no dedicated test coverage —
// abs must correctly negate negative values and pass through positive ones,
// while round must handle negative midpoints, positive midpoints, and integers.
func TestAbsAndRound(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("abs_neg=" + str(abs(-5.0)))
    println("abs_pos=" + str(abs(5.0)))
    println("abs_zero=" + str(abs(0.0)))
    println("round_pos=" + str(round(2.5)))
    println("round_neg=" + str(round(-2.5)))
    println("round_int=" + str(round(3.0)))
    println("round_below=" + str(round(2.4)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"abs_neg=5", "abs_pos=5", "abs_zero=0",
		"round_pos=3", "round_neg=-3", "round_int=3", "round_below=2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
