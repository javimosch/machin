package main

import (
	"strings"
	"testing"
)

// These tests exercise the union-failure error branches inside tryRange
// (types.go) for each range-able kind: slice, string, and map. Each case
// assigns a loop variable a value incompatible with the type tryRange
// infers for it (e.g. slice index must be int, map value must match the
// map's value type), forcing the union() call inside tryRange to fail.

func TestRangeSliceKeyTypeConflict(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := []int{1, 2, 3} for i := range s { i = "oops" } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected type mismatch error, got %v", err)
	}
}

func TestRangeSliceValueTypeConflict(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := []int{1, 2, 3} for i, v := range s { v = "oops" println(i) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected type mismatch error, got %v", err)
	}
}

func TestRangeStringKeyTypeConflict(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := "hi" for i := range s { i = "oops" } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected type mismatch error, got %v", err)
	}
}

func TestRangeStringValueTypeConflict(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := "hi" for i, c := range s { c = 5 println(i) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected type mismatch error, got %v", err)
	}
}

func TestRangeMapKeyTypeConflict(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { m := make(map[string]int) m["a"] = 1 for k := range m { k = 5 } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected type mismatch error, got %v", err)
	}
}

func TestRangeMapValueTypeConflict(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { m := make(map[string]int) m["a"] = 1 for k, v := range m { v = "oops" println(k) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected type mismatch error, got %v", err)
	}
}
