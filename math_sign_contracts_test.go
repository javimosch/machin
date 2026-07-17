package main

import (
	"strings"
	"testing"
)

// The existing math-builtin tests (mathstr2/mathstr3) only exercise the
// positive, happy-path branch of each function: cbrt(27), fmod(7,3),
// pow(2,10), hypot(3,4), etc. The subtle, regression-prone behavior of these
// libm-backed builtins is how they treat *signs* and non-integer exponents —
// exactly the cases a wrong-libm-call or a naive reimplementation gets wrong:
//
//   - cbrt is defined for negatives (unlike sqrt): cbrt(-27) == -3.
//   - pow with a negative base + odd integer exponent stays negative; a
//     negative exponent is a reciprocal; a 0.5 exponent is a square root; any
//     base to the 0 power is 1.
//   - fmod follows the *dividend's* sign (C99 semantics), not the divisor's —
//     a Go `%`-style or abs-based implementation would flip fmod(-7,3).
//   - trunc rounds toward zero while floor rounds toward -inf: for a negative
//     operand they differ by one (trunc(-3.9)==-3 vs floor(-3.9)==-4), which
//     pins that trunc is not secretly implemented via floor.
//   - hypot is sign-independent (a magnitude): hypot(-3,4) == 5.
//
// None of these signed/edge contracts are covered elsewhere, so a regression
// in any one of them would otherwise slip through silently.
func TestMathSignAndExponentContracts(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    // cbrt of a negative — well-defined, unlike sqrt.
    println("cbrt_neg=" + str(cbrt(-27.0)))

    // pow: negative base with odd exponent, negative exponent (reciprocal),
    // fractional exponent (square root), and the x^0 == 1 identity.
    println("pow_negbase_odd=" + str(pow(-2.0, 3.0)))
    println("pow_negexp=" + str(pow(2.0, -1.0)))
    println("pow_half=" + str(pow(9.0, 0.5)))
    println("pow_zero=" + str(pow(5.0, 0.0)))

    // fmod result takes the sign of the dividend (the first argument).
    println("fmod_negdividend=" + str(fmod(-7.0, 3.0)))
    println("fmod_negdivisor=" + str(fmod(7.0, -3.0)))

    // trunc rounds toward zero; floor rounds toward -inf — they disagree on
    // negatives, which proves trunc is not floor in disguise.
    println("trunc_neg=" + str(trunc(-3.9)))
    println("floor_neg=" + str(floor(-3.9)))
    println("ceil_neg=" + str(ceil(-3.1)))

    // hypot is a magnitude: independent of argument signs.
    println("hypot_neg=" + str(hypot(-3.0, 4.0)))

    // exp and log are inverses: log(exp(1)) round-trips back to 1.
    println("explog_roundtrip=" + str(log(exp(1.0))))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"cbrt_neg=-3",
		"pow_negbase_odd=-8",
		"pow_negexp=0.5",
		"pow_half=3",
		"pow_zero=1",
		"fmod_negdividend=-1",
		"fmod_negdivisor=1",
		"trunc_neg=-3",
		"floor_neg=-4",
		"ceil_neg=-3",
		"hypot_neg=5",
		"explog_roundtrip=1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
