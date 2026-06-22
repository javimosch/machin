package main

import (
	"strings"
	"testing"
)

// TestFFIMath exercises Phase-1 C FFI end to end: an extern declaration with a
// header + link, a direct call to the foreign function, and linking via -lm.
func TestFFIMath(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "m" { header "math.h" link "m" fn sqrt(float) float fn pow(float, float) float }`,
		`func main(){ println(sqrt(2.0)) println(pow(2.0, 10.0)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "1.41421\n1024\n"; out != want {
		t.Fatalf("FFI math: got %q, want %q", out, want)
	}
}

// A foreign call with the wrong argument count is a clean MFL type error, not a
// leaked cc failure.
func TestFFIArgCountChecked(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "m" { header "math.h" link "m" fn sqrt(float) float }`,
		`func main(){ println(sqrt(1.0, 2.0)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "expected 1 args") {
		t.Fatalf("expected an arg-count error, got %v", err)
	}
}
