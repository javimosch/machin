package main

import (
	"strings"
	"testing"
)

// TestMultiAssignCommaOkReceive exercises the comma-ok receive case in
// genMultiAssign where a channel receive yields (value, ok bool).
func TestMultiAssignCommaOkReceive(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) go send(c) v, ok := <-c println(v) println(ok) }`,
		`func send(c) { c <- 42 close(c) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignCommaOkReceiveWrongArity rejects comma-ok receive with
// wrong number of variables.
func TestMultiAssignCommaOkReceiveWrongArity(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) v, ok, extra := <-c }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "comma-ok receive needs exactly 2 variables") {
		t.Fatalf("expected comma-ok arity error, got %v", err)
	}
}

// TestMultiAssignBlankIdentifier exercises the blank `_` identifier in
// comma-ok receive context (discarding the value).
func TestMultiAssignBlankIdentifier(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) go send(c) _, ok := <-c println(ok) }`,
		`func send(c) { c <- 42 close(c) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignBlankAndValue exercises the blank identifier alongside
// a normal variable binding (keeping the value, discarding the ok flag).
func TestMultiAssignBlankAndValue(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan string) go sendstr(c) v, _ := <-c println(v) }`,
		`func sendstr(c) { c <- "hello" close(c) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignGlobals exercises `=` multi-assign into package globals:
// names missing from the local env resolve through globalSlot, the same
// fallback single `=` assign has (#648).
func TestMultiAssignGlobals(t *testing.T) {
	prog, err := ParseProgram([]string{
		`var gx = 0`,
		`var gy = ""`,
		`func main() { gx, gy = 1, "s"  println(gx)  println(gy) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignArityDiagnostics exercises the error-message helpers for
// multi-assign arity mismatches: single-value RHS, builtin call RHS, and
// user-function RHS each take a different describeSingleRHS branch.
func TestMultiAssignArityDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		srcs []string
		want string
	}{
		{"expr-rhs", []string{`func main() { a := 0  b := 0  a, b = 1 + 2 }`}, "single value"},
		{"builtin-rhs", []string{`func main() { xs := []int{1}  a := 0  b := 0  a, b = len(xs) }`}, "yields a single value"},
		{"user-rhs", []string{`func one() { return 1 }`, `func main() { a := 0  b := 0  a, b = one() }`}, "returns 1 value"},
	}
	for _, tc := range cases {
		decls := make([]string, len(tc.srcs))
		for i, s := range tc.srcs {
			decls[i] = normalize(s)
		}
		prog, err := ParseProgram(decls)
		if err != nil {
			t.Fatalf("%s parse: %v", tc.name, err)
		}
		_, err = Check(prog)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected %q in error, got %v", tc.name, tc.want, err)
		}
	}
}

// TestMultiAssignUndefinedGlobalFallback rejects `=` multi-assign to names
// that are neither locals nor package globals.
func TestMultiAssignUndefinedGlobalFallback(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { x, y = 1, 2 }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "assignment to undefined variable") {
		t.Fatalf("expected undefined-variable error, got %v", err)
	}
}
