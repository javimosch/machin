package main

import (
	"strings"
	"testing"
)

// TestStructLitUnknownNamedField exercises genStructLit's error path when a
// named-field literal references a field the struct doesn't declare.
func TestStructLitUnknownNamedField(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type Point struct { x int y int }`,
		`func main() { p := Point{x: 1, z: 2} println(p) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "has no field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

// TestStructLitPositionalArityMismatch exercises genStructLit's error path
// when a positional literal supplies the wrong number of values.
func TestStructLitPositionalArityMismatch(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type Point struct { x int y int }`,
		`func main() { p := Point{1} println(p) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "expected 2 fields, got 1") {
		t.Fatalf("expected field-count mismatch error, got %v", err)
	}
}
