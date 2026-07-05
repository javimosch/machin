package main

import (
	"strings"
	"testing"
)

func checkerFromProgram(t *testing.T, decls ...string) *Checker {
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	return c
}

// TestTypeSlotCompositeAndScalar exercises the typeSlot recursion through
// []T, chan T, and map[K]V, plus its struct-name and scalar leaves.
func TestTypeSlotCompositeAndScalar(t *testing.T) {
	c := checkerFromProgram(t, `type P struct { x int  y int }`, `func main() { println(1) }`)

	cases := []string{"int", "float", "bool", "string", "bytes", "[]int", "chan int", "map[string]int", "P"}
	for _, tn := range cases {
		if _, err := c.typeSlot(tn); err != nil {
			t.Errorf("typeSlot(%q): unexpected error: %v", tn, err)
		}
	}
}

// TestTypeSlotUnknownType exercises the typeSlot error leaf for a type name
// that is neither a builtin scalar nor a declared struct.
func TestTypeSlotUnknownType(t *testing.T) {
	c := checkerFromProgram(t, `func main() { println(1) }`)

	if _, err := c.typeSlot("NoSuchType"); err == nil {
		t.Fatal("typeSlot(\"NoSuchType\"): expected error, got nil")
	}
}

// TestCheckTypeNameAccepts exercises the checkTypeName recursion through
// []T, chan T, and map[K]V, plus its struct-name and scalar leaves.
func TestCheckTypeNameAccepts(t *testing.T) {
	c := checkerFromProgram(t, `type P struct { x int  y int }`, `func main() { println(1) }`)

	cases := []string{"int", "float", "bool", "string", "bytes", "func", "[]int", "chan int", "map[string]int", "P"}
	for _, tn := range cases {
		if err := c.checkTypeName(tn); err != nil {
			t.Errorf("checkTypeName(%q): unexpected error: %v", tn, err)
		}
	}
}

// TestCheckTypeNameRejectsUnknown exercises the checkTypeName error leaf,
// including through a composite type whose element is unknown.
func TestCheckTypeNameRejectsUnknown(t *testing.T) {
	c := checkerFromProgram(t, `func main() { println(1) }`)

	for _, tn := range []string{"NoSuchType", "[]NoSuchType", "map[string]NoSuchType"} {
		err := c.checkTypeName(tn)
		if err == nil {
			t.Errorf("checkTypeName(%q): expected error, got nil", tn)
			continue
		}
		if !strings.Contains(err.Error(), "unknown type") {
			t.Errorf("checkTypeName(%q): error = %v, want mention of unknown type", tn, err)
		}
	}
}
