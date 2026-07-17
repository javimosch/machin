package main

import (
	"strings"
	"testing"
)

// cos and tan are wired into codegen (mfl_math_cos / mfl_math_tan) but had no
// dedicated numeric test — only sin was pinned (mathstr_builtins_test.go), and
// the inverses asin/acos/atan are covered elsewhere. Each forward function is a
// single-arg pure builtin, but a swapped-libm-function or dropped-arg
// regression in codegen would otherwise slip through silently. Expected strings
// match the C runtime's "%g" formatting (6 significant digits).
func TestCosBuiltin(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("cos0=" + str(cos(0.0)))
    println("cosPi=" + str(cos(pi())))
    println("cos1=" + str(cos(1.0)))
    println("cosNeg1=" + str(cos(0.0-1.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"cos0=1",           // cos(0) == 1
		"cosPi=-1",         // cos(pi) == -1
		"cos1=0.540302",    // cos(1 rad)
		"cosNeg1=0.540302", // even function: cos(-x) == cos(x)
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestTanBuiltin pins tan at its clean landmarks plus its odd-function
// symmetry, which distinguishes a correct tan from an accidental cos/sin wiring.
func TestTanBuiltin(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("tan0=" + str(tan(0.0)))
    println("tanQuarterPi=" + str(tan(pi()/4.0)))
    println("tan1=" + str(tan(1.0)))
    println("tanNeg1=" + str(tan(0.0-1.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"tan0=0",           // tan(0) == 0
		"tanQuarterPi=1",   // tan(pi/4) == 1
		"tan1=1.55741",     // tan(1 rad)
		"tanNeg1=-1.55741", // odd function: tan(-x) == -tan(x)
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
