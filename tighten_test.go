package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tighten drops whitespace adjacent to operators/punctuation but must keep the
// one space the lexer needs between two word tokens, and must never touch the
// contents of a string literal.
func TestTighten(t *testing.T) {
	cases := []struct{ in, want string }{
		{"func fib(n) { return fib(n - 1) + fib(n - 2) }", "func fib(n){return fib(n-1)+fib(n-2)}"},
		{"acc := acc * i", "acc:=acc*i"},
		{"return n", "return n"},                     // word-word space is significant — kept
		{"else if i % 3 == 0", "else if i%3==0"},     // keyword spaces kept; operator spaces dropped
		{`println("a, b  c")`, `println("a, b  c")`}, // spaces inside a string are preserved
		{`x := "hi" + "  y"`, `x:="hi"+"  y"`},       // tighten around +, keep string interiors
	}
	for _, c := range cases {
		if got := tighten(c.in); got != c.want {
			t.Errorf("tighten(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Every committed example is already in canonical (tight) form: tighten is a
// no-op on it. This guards the repo's source of truth against drift.
func TestExamplesAreCanonical(t *testing.T) {
	files, _ := filepath.Glob("examples/**/*.mfl")
	more, _ := filepath.Glob("examples/*.mfl")
	for _, f := range append(files, more...) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if got := tighten(line); got != line {
				t.Errorf("%s is not canonical:\n  have %q\n  want %q", f, line, got)
			}
		}
	}
}
