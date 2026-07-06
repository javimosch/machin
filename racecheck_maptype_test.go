package main

import "testing"

// TestMapValTypeSimple covers the common "map[K]V" case with an unbracketed key.
func TestMapValTypeSimple(t *testing.T) {
	if got := mapValType("map[string]int"); got != "int" {
		t.Fatalf("mapValType(map[string]int) = %q, want %q", got, "int")
	}
}

// TestMapValTypeBracketedKey covers a key type that itself contains brackets
// (e.g. a slice key), exercising the depth-tracking loop.
func TestMapValTypeBracketedKey(t *testing.T) {
	if got := mapValType("map[[]int]string"); got != "string" {
		t.Fatalf("mapValType(map[[]int]string) = %q, want %q", got, "string")
	}
}

// TestMapValTypeMalformed covers the fallback "?" return when the key bracket
// is never closed.
func TestMapValTypeMalformed(t *testing.T) {
	if got := mapValType("map[string"); got != "?" {
		t.Fatalf("mapValType(map[string) = %q, want %q", got, "?")
	}
}
