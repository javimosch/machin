package main

import "testing"

// isExternDirective checks if a word is an extern directive keyword
// (header, link, cflags, fn, cstruct); these cannot be used as return types.
func TestIsExternDirective(t *testing.T) {
	cases := []struct {
		word string
		want bool
	}{
		{"header", true},
		{"link", true},
		{"cflags", true},
		{"fn", true},
		{"cstruct", true},
		{"int", false},
		{"string", false},
		{"myType", false},
		{"", false},
		{"FN", false},        // case-sensitive
		{"Header", false},    // case-sensitive
	}
	for _, c := range cases {
		if got := isExternDirective(c.word); got != c.want {
			t.Errorf("isExternDirective(%q) = %v, want %v", c.word, got, c.want)
		}
	}
}

// ParseExtern's own Lex error path, and parseExternDecl's directive-argument
// error branches (header/link/cflags/cstruct each expect a specific token
// after the keyword), had zero test coverage.
func TestParseExternLexError(t *testing.T) {
	if _, err := ParseExtern(`extern "m { fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected a lex error for an unterminated string, got nil")
	}
}

func TestParseExternHeaderMissingString(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { header 123 fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for header directive missing a string, got nil")
	}
}

func TestParseExternLinkMissingString(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { link 123 fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for link directive missing a string, got nil")
	}
}

func TestParseExternCflagsMissingString(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { cflags 123 fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for cflags directive missing a string, got nil")
	}
}

func TestParseExternCstructMissingName(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { cstruct 123 { } fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for cstruct directive missing a name, got nil")
	}
}
