package main

import (
	"strings"
	"testing"
)

// log2/log10/acos/trunc/pow/atan2/hypot and to_upper had no test coverage —
// each is a small pure builtin, but a broken codegen case (wrong libm
// function, swapped arg order) would otherwise slip through silently.
func TestEvenMoreMathAndStringBuiltins(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("log2_8=" + str(log2(8.0)))
    println("log10_100=" + str(log10(100.0)))
    println("acos1=" + str(acos(1.0)))
    println("trunc=" + str(trunc(3.9)))
    println("pow=" + str(pow(2.0, 10.0)))
    println("atan2=" + str(atan2(0.0, 1.0)))
    println("hypot=" + str(hypot(3.0, 4.0)))
    println("upper=" + to_upper("HeLLo"))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"log2_8=3", "log10_100=2", "acos1=0", "trunc=3", "pow=1024", "atan2=0", "hypot=5", "upper=HELLO",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
