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

// Test to_upper edge cases: empty string, numbers, symbols, and all-uppercase input.
// These edge cases ensure the function doesn't regress on non-alphabetic content.
func TestToUpperEdgeCases(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("empty=[" + to_upper("") + "]")
    println("numbers=[" + to_upper("abc123") + "]")
    println("symbols=[" + to_upper("abc!@#") + "]")
    println("already_upper=[" + to_upper("HELLO") + "]")
    println("mixed=[" + to_upper("teSt_123!") + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"empty=[]",
		"numbers=[ABC123]",
		"symbols=[ABC!@#]",
		"already_upper=[HELLO]",
		"mixed=[TEST_123!]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
