package main

import (
	"strings"
	"testing"
)

// ceil/floor/sqrt are foundational math functions that had no dedicated test
// coverage — only exercised indirectly through broader integration tests.
// Lock in correct rounding behavior and handling of edge cases.
func TestCeilFloorSqrt(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("ceil_frac=" + str(ceil(2.1)))
    println("ceil_int=" + str(ceil(3.0)))
    println("ceil_neg=" + str(ceil(-2.1)))
    println("floor_frac=" + str(floor(2.9)))
    println("floor_int=" + str(floor(3.0)))
    println("floor_neg=" + str(floor(-2.9)))
    println("sqrt_4=" + str(sqrt(4.0)))
    println("sqrt_2=" + str(sqrt(2.0)))
    println("sqrt_0=" + str(sqrt(0.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"ceil_frac=3", "ceil_int=3", "ceil_neg=-2",
		"floor_frac=2", "floor_int=3", "floor_neg=-3",
		"sqrt_4=2", "sqrt_2=1.41421", "sqrt_0=0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
