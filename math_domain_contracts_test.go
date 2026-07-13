package main

import (
	"strings"
	"testing"
)

// The existing math builtin tests (mathstr2/mathstr3) only exercise the
// happy path — cbrt(27)=3, fmod(7,3)=1, hypot(3,4)=5, pow(...)=1024. Those
// pass even if codegen picks the wrong libm entry point or mishandles sign,
// because every input is positive and every result is a clean integer. The
// contracts below pin the *distinguishing* numeric behavior — the cases where
// a subtly-wrong implementation (e.g. cbrt routed through sqrt, fmod taking
// the sign of the divisor, pow special-casing 0^0 differently) would produce
// a different answer.

// cbrt is the real cube root: unlike sqrt, it is defined for negatives and
// returns a negative result. cbrt(-27) = -3, not NaN and not +3.
func TestCbrtNegativeAndZeroContract(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("neg27=" + str(cbrt(0.0 - 27.0)))
    println("neg8=" + str(cbrt(0.0 - 8.0)))
    println("zero=" + str(cbrt(0.0)))
    println("one=" + str(cbrt(1.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"neg27=-3", "neg8=-2", "zero=0", "one=1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("cbrt: missing %q in:\n%s", want, out)
		}
	}
}

// fmod follows C: the result takes the sign of the dividend (the first arg),
// never the divisor. fmod(-7,3) = -1 (not +2, which is what a mod that follows
// the divisor's sign would give); fmod(7,-3) = +1.
func TestFmodSignOfDividendContract(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("negdiv=" + str(fmod(0.0 - 7.0, 3.0)))
    println("negmod=" + str(fmod(7.0, 0.0 - 3.0)))
    println("frac=" + str(fmod(5.5, 2.0)))
    println("exact=" + str(fmod(6.0, 3.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"negdiv=-1", "negmod=1", "frac=1.5", "exact=0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("fmod: missing %q in:\n%s", want, out)
		}
	}
}

// hypot(x,y) = sqrt(x*x + y*y): symmetric in its arguments, insensitive to the
// sign of either, and equal to |x| when the other leg is zero. These pin that
// the two args aren't accidentally swapped-sensitive or sign-sensitive.
func TestHypotSymmetryAndZeroContract(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("zerozero=" + str(hypot(0.0, 0.0)))
    println("negx=" + str(hypot(0.0 - 3.0, 4.0)))
    println("swap=" + str(hypot(4.0, 3.0)))
    println("legzero=" + str(hypot(5.0, 0.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"zerozero=0", "negx=5", "swap=5", "legzero=5",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("hypot: missing %q in:\n%s", want, out)
		}
	}
}

// pow identities: anything to the 0 power is 1 (including 0^0, the C/IEEE
// convention), a negative exponent yields a reciprocal, a negative base with an
// odd integer exponent stays negative, and a 0.5 exponent is the square root.
func TestPowIdentityContract(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("x0=" + str(pow(7.0, 0.0)))
    println("zerozero=" + str(pow(0.0, 0.0)))
    println("negexp=" + str(pow(2.0, 0.0 - 2.0)))
    println("negbase=" + str(pow(0.0 - 2.0, 3.0)))
    println("half=" + str(pow(9.0, 0.5)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"x0=1", "zerozero=1", "negexp=0.25", "negbase=-8", "half=3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("pow: missing %q in:\n%s", want, out)
		}
	}
}
