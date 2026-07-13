package main

import (
	"os"
	"os/exec"
	"testing"
)

// runNativeSafe compiles the given function sources with the --safe checks
// enabled (bounds/overflow), runs the binary, and returns its combined stdout.
// The bool result reports whether the process exited cleanly (no panic/abort).
func runNativeSafe(t *testing.T, funcs ...string) (string, bool) {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		fn, err := ParseFunc(normalize(f))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	bin, err := os.CreateTemp("", "mfl-sc-*")
	if err != nil {
		t.Fatalf("tempfile: %v", err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: fns}, bin.Name(), true); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	return string(out), err == nil
}

// TestShortCircuitAvoidsSideEffect verifies that && / || short-circuit: the
// right operand is not evaluated when the left already decides the result.
// Regression for #437, where seqExprs hoisted both operands into temporaries
// whenever either had a side effect, evaluating the right operand always.
func TestShortCircuitAvoidsSideEffect(t *testing.T) {
	got := runNative(t,
		`func touch() (b) { println("touched") b = true }`,
		`func main() {
			if false && touch() { println("A") } else { println("B") }
			if true || touch() { println("C") }
		}`)
	const want = "B\nC\n"
	if got != want {
		t.Fatalf("got %q, want %q (right operand should not be evaluated)", got, want)
	}
}

// TestShortCircuitInAssignment covers the same short-circuit guarantee in
// value context (an assignment rather than an if-condition).
func TestShortCircuitInAssignment(t *testing.T) {
	got := runNative(t,
		`func touch() (b) { println("touched") b = false }`,
		`func main() {
			x := true || touch()
			y := false && touch()
			println(x, y)
		}`)
	const want = "true false\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestShortCircuitSafeIndexGuard reproduces issue #437 directly: under --safe,
// a guarding condition on the left of && must prevent the crashing call on the
// right from running at all.
func TestShortCircuitSafeIndexGuard(t *testing.T) {
	out, ok := runNativeSafe(t,
		`func boom(xs) (v) { v = xs[0 - 1] }`,
		`func main() {
			xs := []string{"a", "b"}
			j := 0
			if j > 0 && boom(xs) == "a" { println("entered") } else { println("skipped") }
			println("survived")
		}`)
	if !ok {
		t.Fatalf("program aborted (short-circuit did not guard boom); output=%q", out)
	}
	const want = "skipped\nsurvived\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}
