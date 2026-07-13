package main

import (
	"strings"
	"testing"
)

// atan and atan2 are wired into codegen (mfl_math_atan / mfl_math_atan2) but
// had no dedicated test coverage. atan is single-arg; atan2 is two-arg and
// must respect the sign of BOTH operands to place the angle in the correct
// quadrant — a swapped-argument or dropped-sign regression in codegen would
// otherwise slip through silently. Expected strings match the C runtime's
// "%g" formatting (6 significant digits).
func TestAtanBuiltin(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("atan0=" + str(atan(0.0)))
    println("atan1=" + str(atan(1.0)))
    println("atanNeg1=" + str(atan(-1.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"atan0=0",
		"atan1=0.785398",     // pi/4
		"atanNeg1=-0.785398", // -pi/4, odd function
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestAtan2Quadrants pins atan2's quadrant resolution: all four (y,x) sign
// combinations, plus the axis cases that distinguish atan2 from a plain
// atan(y/x).
func TestAtan2Quadrants(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("q1=" + str(atan2(1.0, 1.0)))
    println("q2=" + str(atan2(1.0, -1.0)))
    println("q3=" + str(atan2(-1.0, -1.0)))
    println("q4=" + str(atan2(-1.0, 1.0)))
    println("piY=" + str(atan2(0.0, -1.0)))
    println("halfpi=" + str(atan2(1.0, 0.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"q1=0.785398",   //  pi/4  (+,+)
		"q2=2.35619",    //  3pi/4 (+,-)
		"q3=-2.35619",   // -3pi/4 (-,-)
		"q4=-0.785398",  // -pi/4  (-,+)
		"piY=3.14159",   //  pi    on the negative x-axis
		"halfpi=1.5708", //  pi/2  on the positive y-axis
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
