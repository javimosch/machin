package main

import (
	"strings"
	"testing"
)

// C trigraphs (e.g. "??'" -> '^') must not survive into the generated C, or the
// toolchain silently corrupts string literals containing "??" (#508).
func TestCStringLitEscapesTrigraphs(t *testing.T) {
	got := cStringLit("A??'B ??( ??) ??= END")
	if strings.Contains(got, "??") {
		t.Fatalf("cStringLit left an unescaped trigraph sequence: %q", got)
	}
	if !strings.Contains(got, "\\?\\?") {
		t.Fatalf("cStringLit did not escape '?' as '\\?': %q", got)
	}
}

// Strings without question marks must be quoted exactly as before.
func TestCStringLitLeavesPlainStrings(t *testing.T) {
	got := cStringLit("hello world")
	if got != `"hello world"` {
		t.Fatalf("cStringLit(\"hello world\") = %q, want %q", got, `"hello world"`)
	}
}
