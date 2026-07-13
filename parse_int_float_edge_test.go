package main

import "testing"

// parse_int / parse_float lower to C's strtoll(s, NULL, 10) / strtod(s, NULL).
// That pins a specific, easy-to-regress contract: leading whitespace is skipped,
// a leading '+'/'-' sign is honored, and parsing stops at the first character
// that cannot extend the number (a "partial parse") rather than erroring. The
// existing suite only covers the clean cases ("8080", "-42", "x", "") — these
// tests nail down the whitespace / sign / partial-parse edges so a future swap
// to a stricter parser can't silently change behavior.
//
// Each value below was confirmed against the native runtime before pinning.
func TestParseIntStrtollContract(t *testing.T) {
	cases := []struct {
		in   string // MFL string literal passed to parse_int
		want string // exact println output
	}{
		{`"  42"`, "42"},  // leading spaces are skipped
		{`"\t5"`, "5"},    // leading tab is whitespace too
		{`"12abc"`, "12"}, // partial parse: stops at first non-digit
		{`"+7"`, "7"},     // explicit '+' sign
		{`"  -9"`, "-9"},  // whitespace then a negative sign
		{`"0x10"`, "0"},   // base 10 only: reads the leading 0, stops at 'x'
		{`"3.9"`, "3"},    // integer parse stops at the '.'
	}
	for _, c := range cases {
		src := `func main() { println(parse_int(` + c.in + `)) }`
		if got := runNative(t, src); got != c.want+"\n" {
			t.Errorf("parse_int(%s): got %q, want %q", c.in, got, c.want+"\n")
		}
	}
}

// parse_float mirrors parse_int's tolerance via strtod: whitespace-skipping,
// scientific notation, a bare leading '.', partial parses, and 0 on empty input.
func TestParseFloatStrtodContract(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"3.14xyz"`, "3.14"}, // partial parse: trailing junk ignored
		{`"1e3"`, "1000"},     // scientific notation, printed without a fraction
		{`".5"`, "0.5"},       // a leading '.' is a valid float
		{`"  2.5"`, "2.5"},    // leading whitespace is skipped
		{`""`, "0"},           // empty input yields 0, not an error
		{`"-0.25"`, "-0.25"},  // negative fractional value round-trips
	}
	for _, c := range cases {
		src := `func main() { println(parse_float(` + c.in + `)) }`
		if got := runNative(t, src); got != c.want+"\n" {
			t.Errorf("parse_float(%s): got %q, want %q", c.in, got, c.want+"\n")
		}
	}
}
