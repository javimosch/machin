package main

import (
	"strings"
	"testing"
)

// parse_float parses a string to a double (strtod; 0.0 on garbage) — the float
// counterpart to parse_int, e.g. to carry a SQLite REAL value into a Mongo double.
// f64_bits / f64_from_bits reinterpret a double's IEEE-754 bits as an int64 and back.
func TestParseFloatAndF64Bits(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("pf=" + str(parse_float("3.5") + 0.5))    // 4
    println("pi=" + str(parse_float("3.14159")))
    println("bad=" + str(parse_float("nope")))         // 0
    b := f64_bits(2.5)
    println("rt=" + str(f64_from_bits(b)) + " same=" + str(f64_from_bits(b) == 2.5))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"pf=4", "pi=3.14159", "bad=0", "rt=2.5 same=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The 2.5 round trip above never exercises the sign bit or the zero bit
// pattern, both easy to get wrong in a hand-rolled IEEE-754 reinterpret.
func TestF64BitsRoundTripNegativeAndZero(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    neg := f64_bits(-2.5)
    println("neg=" + str(f64_from_bits(neg) == -2.5))
    zero := f64_bits(0.0)
    println("zero=" + str(f64_from_bits(zero) == 0.0))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"neg=true", "zero=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestParseFloatEdgeCases covers empty string (parses to 0.0) and the literal
// "0" (must parse to 0.0, not garbage).
func TestParseFloatEdgeCases(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    empty := parse_float("")
    println("empty=" + str(empty == 0.0))
    zero := parse_float("0")
    println("zero=" + str(zero == 0.0))
    zero_dot := parse_float("0.0")
    println("zero_dot=" + str(zero_dot == 0.0))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"empty=true", "zero=true", "zero_dot=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
