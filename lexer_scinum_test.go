package main

import "testing"

// The decimal number scanner accepts an optional exponent (scientific notation):
// e/E, an optional +/- sign, then one or more digits. The mantissa may be
// integer-only, so `1e3` is a float. Before this the scanner stopped at 'e', so
// `1.5e3` mis-lexed as [1.5, e3] and any exponent literal was a parse error.

func TestLexScientificNotationSingleFloatToken(t *testing.T) {
	cases := []string{
		"1e3",
		"1E3",
		"1e+3",
		"1e-3",
		"1.5e3",
		"1.5E3",
		"2.5e-3",
		"2.5e+3",
		"6.022e23",
		"1_000e1_0", // underscores are separators in both mantissa and exponent
	}
	for _, src := range cases {
		toks, err := Lex(src)
		if err != nil {
			t.Errorf("Lex(%q) error: %v", src, err)
			continue
		}
		// Expect exactly one real token plus TEOF.
		if len(toks) != 2 || toks[1].Kind != TEOF {
			t.Errorf("Lex(%q) = %d tokens (%+v), want 1 literal + EOF", src, len(toks), toks)
			continue
		}
		if toks[0].Kind != TFloat {
			t.Errorf("Lex(%q) kind = %v, want TFloat", src, toks[0].Kind)
		}
		if toks[0].Val != src {
			t.Errorf("Lex(%q) val = %q, want the raw literal preserved", src, toks[0].Val)
		}
	}
}

// The lexed exponent literal parses to the numerically correct float64.
func TestParseScientificNotationValues(t *testing.T) {
	floats := map[string]float64{
		"1e3":      1000,
		"1e-3":     0.001,
		"1.5e3":    1500,
		"1.5E3":    1500,
		"2.5e-3":   0.0025,
		"2.5e+3":   2500,
		"6.022e23": 6.022e23,
		"1_000e10": 1e13,
	}
	for src, want := range floats {
		toks, err := Lex(src)
		if err != nil {
			t.Fatalf("Lex(%q) error: %v", src, err)
		}
		p := &Parser{toks: toks}
		x, err := p.parsePrimary()
		if err != nil {
			t.Errorf("parse(%q) error: %v", src, err)
			continue
		}
		lit, ok := x.(*FloatLit)
		if !ok {
			t.Errorf("parse(%q) = %T, want *FloatLit", src, x)
			continue
		}
		if lit.Val != want {
			t.Errorf("parse(%q) = %g, want %g", src, lit.Val, want)
		}
	}
}

// The 'e' must only be consumed when a valid exponent (optionally signed digits)
// follows. A bare trailing 'e', or a hex literal whose digits include 'e', must
// NOT be swallowed into a spurious exponent.
func TestLexExponentBoundaries(t *testing.T) {
	// `1e` with no exponent digits: 'e' stays a separate identifier token.
	toks, err := Lex("1e")
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", "1e", err)
	}
	want := []Token{
		{Kind: TInt, Val: "1"},
		{Kind: TIdent, Val: "e"},
		{Kind: TEOF, Val: ""},
	}
	if len(toks) != len(want) {
		t.Fatalf("Lex(%q) = %d tokens (%+v), want %d", "1e", len(toks), toks, len(want))
	}
	for i, w := range want {
		if toks[i].Kind != w.Kind || toks[i].Val != w.Val {
			t.Errorf("token %d = %+v, want Kind=%v Val=%q", i, toks[i], w.Kind, w.Val)
		}
	}

	// Hex literals reach a distinct scanner branch: 0x1e3 is an integer, not a
	// float with an exponent.
	toks, err = Lex("0x1e3")
	if err != nil {
		t.Fatalf("Lex(%q) error: %v", "0x1e3", err)
	}
	if len(toks) != 2 || toks[0].Kind != TInt || toks[0].Val != "0x1e3" {
		t.Errorf("Lex(%q) = %+v, want a single TInt 0x1e3", "0x1e3", toks)
	}
}

// An exponent literal followed by an operator/identifier must still tokenize the
// trailing tokens distinctly — the scanner stops at the end of the digits.
func TestScientificLiteralDoesNotSwallowFollowing(t *testing.T) {
	toks, err := Lex("1.5e3 + x")
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	want := []Token{
		{Kind: TFloat, Val: "1.5e3"},
		{Kind: TOp, Val: "+"},
		{Kind: TIdent, Val: "x"},
		{Kind: TEOF, Val: ""},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens (%+v), want %d", len(toks), toks, len(want))
	}
	for i, w := range want {
		if toks[i].Kind != w.Kind || toks[i].Val != w.Val {
			t.Errorf("token %d = %+v, want Kind=%v Val=%q", i, toks[i], w.Kind, w.Val)
		}
	}
}
