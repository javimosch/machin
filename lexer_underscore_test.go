package main

import "testing"

// The lexer treats '_' as a digit-group separator in decimal integer and
// float literals, consistent with the hex/bin/oct branch and Go's own numeric
// syntax. SPEC §3 documents underscores as separators; before this the decimal
// scanner stopped at '_', so "1_000" mis-lexed as [1, _000].

func TestLexDecimalUnderscoreSingleToken(t *testing.T) {
	cases := []struct {
		src  string
		kind TokKind
	}{
		{"1_000", TInt},
		{"1_000_000", TInt},
		{"0", TInt},
		{"1000", TInt},
		{"3_000.5", TFloat},
		{"1_000.0", TFloat},
		{"0xff_00", TInt}, // hex separators already worked; must stay a single token
		{"0b1010_1010", TInt},
	}
	for _, c := range cases {
		toks, err := Lex(c.src)
		if err != nil {
			t.Errorf("Lex(%q) error: %v", c.src, err)
			continue
		}
		// Expect exactly one real token plus TEOF.
		if len(toks) != 2 || toks[1].Kind != TEOF {
			t.Errorf("Lex(%q) = %d tokens (%+v), want 1 literal + EOF", c.src, len(toks), toks)
			continue
		}
		if toks[0].Kind != c.kind {
			t.Errorf("Lex(%q) kind = %v, want %v", c.src, toks[0].Kind, c.kind)
		}
		if toks[0].Val != c.src {
			t.Errorf("Lex(%q) val = %q, want the raw literal preserved", c.src, toks[0].Val)
		}
	}
}

// The underscores are cosmetic: the parsed numeric value ignores them.
func TestParseUnderscoreLiteralValues(t *testing.T) {
	ints := map[string]int64{
		"1_000":     1000,
		"1_000_000": 1000000,
		"0xff_00":   0xff00,
		"0b1010":    0b1010,
	}
	for src, want := range ints {
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
		lit, ok := x.(*IntLit)
		if !ok {
			t.Errorf("parse(%q) = %T, want *IntLit", src, x)
			continue
		}
		if lit.Val != want {
			t.Errorf("parse(%q) = %d, want %d", src, lit.Val, want)
		}
	}

	floats := map[string]float64{
		"3_000.5": 3000.5,
		"1_000.0": 1000.0,
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

// Malformed underscore placement (doubled, leading, or trailing) is lexed into
// a single literal token but rejected by strconv during parsing, so it surfaces
// as a clean parse error rather than a silently wrong value.
func TestParseMalformedUnderscoreLiteralRejected(t *testing.T) {
	for _, src := range []string{"1__000", "1_", "1_000_", "3__000.5", "3_.5"} {
		toks, err := Lex(src)
		if err != nil {
			// A lex error is an acceptable rejection too.
			continue
		}
		p := &Parser{toks: toks}
		if _, err := p.parsePrimary(); err == nil {
			t.Errorf("parse(%q) succeeded, want a parse error for malformed underscore placement", src)
		}
	}
}

// A digit literal followed immediately by '_' must not swallow a separate
// identifier: within a single number the '_' is a separator, but `1_000 + x`
// still tokenizes the identifier `x` distinctly.
func TestUnderscoreLiteralDoesNotSwallowIdentifier(t *testing.T) {
	toks, err := Lex("1_000 + x")
	if err != nil {
		t.Fatalf("Lex error: %v", err)
	}
	want := []Token{
		{Kind: TInt, Val: "1_000"},
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
