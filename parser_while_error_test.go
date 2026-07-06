package main

import (
	"testing"
)

// TestParseWhileInvalidCondition exercises parseWhile's error path when the
// condition expression itself fails to parse.
func TestParseWhileInvalidCondition(t *testing.T) {
	_, err := ParseProgram([]string{
		`func main() { while * { println("x") } }`,
	})
	if err == nil {
		t.Fatal("expected parse error for invalid while condition, got nil")
	}
}

// TestParseWhileInvalidBody exercises parseWhile's error path when the loop
// body block fails to parse (missing closing brace).
func TestParseWhileInvalidBody(t *testing.T) {
	_, err := ParseProgram([]string{
		`func main() { while true { println("x") }`,
	})
	if err == nil {
		t.Fatal("expected parse error for unterminated while body, got nil")
	}
}
