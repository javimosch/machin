package main

import "testing"

// parseFuncLit's parameter list expects each entry to be a bare identifier;
// a non-identifier token (e.g. a number) must be rejected.
func TestParseFuncLitBadParamToken(t *testing.T) {
	_, err := progFromSrcErr(`
func main() { f := func(1) { println("x") } }
`)
	if err == nil {
		t.Fatal("expected error for non-identifier func literal param, got nil")
	}
}

// A func literal parameter list missing its closing ')' must be rejected.
func TestParseFuncLitMissingCloseParen(t *testing.T) {
	_, err := progFromSrcErr(`
func main() { f := func(a, b { println("x") } }
`)
	if err == nil {
		t.Fatal("expected error for func literal missing ')', got nil")
	}
}

// A func literal missing its body block must be rejected.
func TestParseFuncLitMissingBlock(t *testing.T) {
	_, err := progFromSrcErr(`
func main() { f := func(a, b) }
`)
	if err == nil {
		t.Fatal("expected error for func literal missing body block, got nil")
	}
}

// A struct literal that mixes keyed and positional fields must be rejected.
func TestParseStructLitMixedKeyedPositional(t *testing.T) {
	_, err := progFromSrcErr(`
type Point struct { x int  y int }
func main() { p := Point{x: 1, 2} }
`)
	if err == nil {
		t.Fatal("expected error for struct literal mixing keyed and positional fields, got nil")
	}
}

// A struct literal with an unterminated field list (missing '}') must be
// rejected rather than silently accepted at EOF.
func TestParseStructLitUnterminated(t *testing.T) {
	_, err := progFromSrcErr(`
type Point struct { x int  y int }
func main() { p := Point{x: 1, y: 2 }
`)
	if err == nil {
		t.Fatal("expected error for unterminated struct literal, got nil")
	}
}
