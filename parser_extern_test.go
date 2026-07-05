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
