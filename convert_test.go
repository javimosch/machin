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
