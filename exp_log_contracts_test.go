package main

import (
	"strings"
	"testing"
)

// The exp/log family (exp, log, log2, log10) is the numeric backbone of LLM
// inference: softmax is a sum of exp(), cross-entropy loss is -log(p), and
// entropy/perplexity are measured in log2. The existing mathstr suites only
// smoke each with a single call, so a broken codegen case (wrong libm symbol,
// swapped base, argument mangling) on the composite identities would slip
// through. Pin the exact contracts that inference code relies on.
func TestExpLogNumericContracts(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    // Anchors.
    println("exp0=" + str(exp(0.0)))
    println("exp1=" + str(exp(1.0)))
    println("exp2=" + str(exp(2.0)))
    println("expneg1=" + str(exp(-1.0)))
    println("log1=" + str(log(1.0)))
    println("loge=" + str(log(exp(1.0))))

    // log2 / log10 are exact on powers of their base.
    println("log2_1=" + str(log2(1.0)))
    println("log2_8=" + str(log2(8.0)))
    println("log2_1024=" + str(log2(1024.0)))
    println("log10_1=" + str(log10(1.0)))
    println("log10_1000=" + str(log10(1000.0)))

    // exp and log are mutual inverses.
    println("logexp=" + str(log(exp(3.5))))
    println("explog=" + str(exp(log(7.0))))

    // Homomorphism identities the softmax/log-sum-exp math leans on:
    //   log(a*b) == log(a)+log(b)   and   exp(a+b) == exp(a)*exp(b)
    println("log_mul=" + str(log(6.0) - (log(2.0) + log(3.0))))
    println("exp_add=" + str(exp(5.0) - exp(2.0)*exp(3.0)))

    // Strict monotonicity (both are increasing).
    if exp(1.0) < exp(1.5) { println("mono_exp=yes") }
    if log(2.0) < log(3.0) { println("mono_log=yes") }
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"exp0=1",
		"exp1=2.71828",
		"exp2=7.38906",
		"expneg1=0.367879",
		"log1=0",
		"loge=1",
		"log2_1=0",
		"log2_8=3",
		"log2_1024=10",
		"log10_1=0",
		"log10_1000=3",
		"logexp=3.5",
		"explog=7",
		"log_mul=0",
		"exp_add=0",
		"mono_exp=yes",
		"mono_log=yes",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The numerically-stable softmax subtracts the max logit before exp() so the
// exponentials never overflow. That shift must not change the resulting
// probabilities: softmax(x) == softmax(x - c) for any constant c. Pin that
// invariance end-to-end through exp(), plus the cross-entropy term -log(p) and
// the normalization sum == 1. This ties the exp/log builtins to the real
// inference kernel they exist to serve.
func TestSoftmaxStabilityViaExpLog(t *testing.T) {
	prog := progFromSrc(t, `
func softmax3(a, b, c, shift) (p0, p1, p2) {
    e0 := exp(a - shift)
    e1 := exp(b - shift)
    e2 := exp(c - shift)
    s := e0 + e1 + e2
    p0 = e0 / s
    p1 = e1 / s
    p2 = e2 / s
}
func main() {
    // Same logits, computed unshifted vs shifted by the max (=3).
    u0, u1, u2 := softmax3(1.0, 2.0, 3.0, 0.0)
    s0, s1, s2 := softmax3(1.0, 2.0, 3.0, 3.0)
    println("u=" + str(u0) + "," + str(u1) + "," + str(u2))
    println("s=" + str(s0) + "," + str(s1) + "," + str(s2))
    // The shift leaves probabilities and their sum unchanged.
    println("diff0=" + str(u0 - s0))
    println("sum=" + str(u0 + u1 + u2))
    // Cross-entropy loss when the true class is index 2: -log(p2).
    println("ce=" + str(0.0 - log(u2)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"u=0.0900306,0.244728,0.665241",
		"s=0.0900306,0.244728,0.665241",
		"diff0=0",
		"sum=1",
		"ce=0.407606",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
