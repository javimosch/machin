package main

import "testing"

// isDigit/isAlpha/isAlnum are simple character class helpers used in the lexer
// to classify bytes; they had no direct test coverage despite being foundational
// to tokenization.
func TestCharClassHelpers(t *testing.T) {
	cases := []struct {
		name  string
		c     byte
		digit bool
		alpha bool
		alnum bool
	}{
		{"digit 0", '0', true, false, true},
		{"digit 5", '5', true, false, true},
		{"digit 9", '9', true, false, true},
		{"letter a", 'a', false, true, true},
		{"letter Z", 'Z', false, true, true},
		{"underscore", '_', false, true, true},
		{"space", ' ', false, false, false},
		{"punct !", '!', false, false, false},
		{"punct .", '.', false, false, false},
	}
	for _, c := range cases {
		if got := isDigit(c.c); got != c.digit {
			t.Errorf("isDigit(%q) = %v, want %v", c.c, got, c.digit)
		}
		if got := isAlpha(c.c); got != c.alpha {
			t.Errorf("isAlpha(%q) = %v, want %v", c.c, got, c.alpha)
		}
		if got := isAlnum(c.c); got != c.alnum {
			t.Errorf("isAlnum(%q) = %v, want %v", c.c, got, c.alnum)
		}
	}
}
