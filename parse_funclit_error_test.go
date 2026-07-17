package main

import (
	"strings"
	"testing"
)

// parseFuncLit parses anonymous closures `func(a, b) { ... }` in expression
// position (parser.go). Its happy path is exercised elsewhere; these tests pin
// the four parse-error branches so a regression that swallows one of them (or
// changes its message shape) is caught.

// TestFuncLitValidForms confirms the well-formed closure shapes still parse so
// the error-path tests below are pinning rejection, not a broken happy path.
func TestFuncLitValidForms(t *testing.T) {
	valid := []string{
		`func main() { f := func(a, b) { return } print(f) }`,
		`func main() { f := func() { } print(f) }`,
		`func main() { f := func(a) { println(a) } print(f) }`,
	}
	for _, src := range valid {
		if _, err := ParseProgram([]string{src}); err != nil {
			t.Errorf("expected %q to parse, got error: %v", src, err)
		}
	}
}

// TestFuncLitMissingOpenParen pins the branch where `func` is not followed by
// the mandatory parameter-list open paren.
func TestFuncLitMissingOpenParen(t *testing.T) {
	_, err := ParseProgram([]string{`func main() { f := func { } }`})
	if err == nil {
		t.Fatal("expected error for closure missing '(' after func")
	}
	if !strings.Contains(err.Error(), `expected "("`) {
		t.Errorf("expected missing-'(' message, got: %v", err)
	}
}

// TestFuncLitBadParamToken pins the branch where a parameter position holds a
// non-identifier token (here a numeric literal).
func TestFuncLitBadParamToken(t *testing.T) {
	_, err := ParseProgram([]string{`func main() { f := func(1) { } }`})
	if err == nil {
		t.Fatal("expected error for non-identifier parameter")
	}
	// expect(TIdent, "") reports the offending token value with an empty
	// expected string.
	if !strings.Contains(err.Error(), `got "1"`) {
		t.Errorf("expected bad-param-token message naming '1', got: %v", err)
	}
}

// TestFuncLitMissingCloseParen pins the branch where the parameter list runs
// into the body without a closing paren (the comma/break loop exits, then the
// close-paren expect fails).
func TestFuncLitMissingCloseParen(t *testing.T) {
	_, err := ParseProgram([]string{`func main() { f := func(a { } }`})
	if err == nil {
		t.Fatal("expected error for closure missing ')' after params")
	}
	if !strings.Contains(err.Error(), `expected ")"`) {
		t.Errorf("expected missing-')' message, got: %v", err)
	}
}

// TestFuncLitMissingBlock pins the branch where the closure has a valid
// parameter list but no `{ ... }` body block.
func TestFuncLitMissingBlock(t *testing.T) {
	_, err := ParseProgram([]string{`func main() { f := func(a) }`})
	if err == nil {
		t.Fatal("expected error for closure missing body block")
	}
	if !strings.Contains(err.Error(), `expected "{"`) {
		t.Errorf("expected missing-block message, got: %v", err)
	}
}
