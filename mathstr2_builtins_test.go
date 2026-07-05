package main

import (
	"strings"
	"testing"
)

// asin/cbrt/exp/log/fmod and to_lower had no test coverage at all — each is
// a small pure builtin, but a broken codegen case (wrong arg order, wrong
// libm function) would otherwise slip through silently.
func TestMoreMathAndStringBuiltins(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("asin1=" + str(asin(1.0)))
    println("cbrt27=" + str(cbrt(27.0)))
    println("exp0=" + str(exp(0.0)))
    println("log1=" + str(log(1.0)))
    println("fmod=" + str(fmod(7.0, 3.0)))
    println("lower=" + to_lower("HeLLo"))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"asin1=1.5708", "cbrt27=3", "exp0=1", "log1=0", "fmod=1", "lower=hello",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
