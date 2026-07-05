package main

import (
	"strings"
	"testing"
)

// min and max select the smaller or larger of two values; they had no dedicated
// test coverage. Edge cases: negative numbers, equal values, and type compatibility.
func TestMinMax(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("min_pos=" + str(min(5, 3)))
    println("min_neg=" + str(min(-2, -5)))
    println("min_mixed=" + str(min(-1, 2)))
    println("min_equal=" + str(min(4, 4)))
    println("max_pos=" + str(max(5, 3)))
    println("max_neg=" + str(max(-2, -5)))
    println("max_mixed=" + str(max(-1, 2)))
    println("max_equal=" + str(max(4, 4)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"min_pos=3", "min_neg=-5", "min_mixed=-1", "min_equal=4",
		"max_pos=5", "max_neg=-2", "max_mixed=2", "max_equal=4",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
