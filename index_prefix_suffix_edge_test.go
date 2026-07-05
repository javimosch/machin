package main

import "testing"

// TestIndexNotFoundAndEmptyNeedle covers mfl_index (codegen.go) edge cases not
// exercised by the happy-path assertions in mfl_test.go: a substring that
// isn't present must yield -1, and an empty needle must match at position 0.
func TestIndexNotFoundAndEmptyNeedle(t *testing.T) {
	got := runNative(t, `func main(){ println(index("hello", "zz")) println(index("hello", "")) println(index("", "")) println(index("", "a")) }`)
	if want := "-1\n0\n0\n-1\n"; got != want {
		t.Fatalf("index edge cases: got %q, want %q", got, want)
	}
}

// TestHasPrefixSuffixEdgeCases covers mfl_has_prefix/mfl_has_suffix (codegen.go)
// boundary behavior: an empty prefix/suffix always matches, and a prefix/suffix
// longer than the subject string must not match (guards against strncmp/strcmp
// reading past the shorter buffer).
func TestHasPrefixSuffixEdgeCases(t *testing.T) {
	got := runNative(t, `func main(){ println(has_prefix("hi", "")) println(has_suffix("hi", "")) println(has_prefix("hi", "hello")) println(has_suffix("hi", "hello")) println(has_prefix("", "")) println(has_suffix("", "")) }`)
	if want := "true\ntrue\nfalse\nfalse\ntrue\ntrue\n"; got != want {
		t.Fatalf("has_prefix/has_suffix edge cases: got %q, want %q", got, want)
	}
}
