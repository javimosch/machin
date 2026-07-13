package main

import (
	"strings"
	"testing"
)

// f64_bits reinterprets a double's raw IEEE-754 storage as a signed int64 (a
// bit-for-bit reinterpret, NOT numeric truncation), and f64_from_bits inverts
// it. The existing convert_test.go coverage only checks that a value survives a
// round trip (f64_from_bits(f64_bits(x)) == x); it never pins the *concrete*
// bit layout f64_bits produces. That layout is the whole contract — callers use
// these to hash doubles, to serialize a BSON double (see bson_test.go), and to
// poke float bits into raw memory — so a hand-rolled reinterpret that silently
// swapped byte order, sign-extended wrong, or truncated to int would still pass
// the round-trip tests while corrupting every on-wire value. These pin the
// exact int64 for known doubles so such a regression trips here.
//
// Reference layouts (IEEE-754 binary64, the raw 64 bits read as signed int64):
//
//	 1.0  = 0x3FF0000000000000 =  4607182418800017408
//	 2.0  = 0x4000000000000000 =  4611686018427387904
//	 0.5  = 0x3FE0000000000000 =  4602678819172646912
//	-1.0  = 0xBFF0000000000000 = -4616189618054758400
func TestF64BitsExactLayout(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("one=" + str(f64_bits(1.0)))
    println("two=" + str(f64_bits(2.0)))
    println("half=" + str(f64_bits(0.5)))
    println("negone=" + str(f64_bits(-1.0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"one=4607182418800017408",
		"two=4611686018427387904",
		"half=4602678819172646912",
		"negone=-4616189618054758400",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The sign bit and the exponent field are exactly where a byte-swapped or
// numerically-truncated implementation goes wrong, so pin them directly:
//
//	+0.0        = 0x0000000000000000 =                    0
//	-0.0        = 0x8000000000000000 = -9223372036854775808 (sign bit only; INT64_MIN)
//	+Inf        = 0x7FF0000000000000 =  9218868437227405312
//	-Inf        = 0xFFF0000000000000 =    -4503599627370496
//	5e-324      = 0x0000000000000001 =                    1 (smallest positive subnormal)
//
// Positive and negative zero are numerically equal (0.0 == -0.0) yet must have
// distinct bit patterns — the single most common reinterpret bug is collapsing
// them, which this catches. -0.0 is built as 0.0 * -1.0 to avoid depending on a
// negative-zero literal.
func TestF64BitsSignZeroAndInfinities(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("poszero=" + str(f64_bits(0.0)))
    println("negzero=" + str(f64_bits(0.0 * -1.0)))
    println("posinf=" + str(f64_bits(1.0 / 0.0)))
    println("neginf=" + str(f64_bits(-1.0 / 0.0)))
    println("subnormal=" + str(f64_bits(5e-324)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"poszero=0",
		"negzero=-9223372036854775808",
		"posinf=9218868437227405312",
		"neginf=-4503599627370496",
		"subnormal=1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// f64_from_bits must reconstruct the double from a raw bit pattern — including
// the non-finite ones. Feeding it the pinned integer layouts above must yield
// back the exact values: 0x3FF0000000000000 -> 1.0, and the +Inf pattern must
// produce a value that is larger than the largest finite double (1e308), i.e.
// an actual infinity, not a garbage large-but-finite number. This closes the
// loop the other direction from f64_bits and guards f64_from_bits' own
// reinterpret path on the value classes round-trip-of-a-normal never reaches.
func TestF64FromBitsReconstructsSpecials(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("one=" + str(f64_from_bits(4607182418800017408) == 1.0))
    println("half=" + str(f64_from_bits(4602678819172646912) == 0.5))
    inf := f64_from_bits(9218868437227405312)
    println("inf=" + str(inf > 1.0e308))
    println("infrt=" + str(f64_bits(inf) == 9218868437227405312))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"one=true", "half=true", "inf=true", "infrt=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
