package main

import (
	"strings"
	"testing"
)

// A type-mismatch error used to read only "type mismatch: string vs slice" —
// naming neither the offending identifier nor a location, forcing a manual
// bisect to find it (issue #506). These tests pin the enriched form: the message
// names the variable and its enclosing function, and `check` maps that function
// to a source line.

func TestMismatchNamesIdentifierAndFunc(t *testing.T) {
	// `steps` is a string in one branch and a slice in the other (both bind the
	// same function-scoped slot — see #507), so unification fails.
	prog, err := ParseProgram([]string{
		`func main() { a := args() if len(a) > 1 { steps := "a,b,c" println(steps) } else { steps := split("a,b,c", ",") println(steps[0]) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil {
		t.Fatal("expected a type mismatch error, got nil")
	}
	msg := cerr.Error()
	if !strings.Contains(msg, "type mismatch") {
		t.Fatalf("message lost its type-mismatch wording: %q", msg)
	}
	if !strings.Contains(msg, "'steps'") {
		t.Fatalf("message does not name the offending identifier: %q", msg)
	}
	if !strings.Contains(msg, `"main"`) {
		t.Fatalf("message does not name the enclosing function: %q", msg)
	}
}

// The `check` command should surface the enclosing declaration + its start line
// (via locateCheckError) now that the mismatch message quotes the function name.
func TestMismatchCheckCarriesDeclAndLine(t *testing.T) {
	src := "func main() {\n" +
		"\tsteps := \"a,b,c\"\n" +
		"\tsteps = split(\"a,b,c\", \",\")\n" +
		"\tprintln(steps[0])\n" +
		"}\n"
	res := analyzeSource(src, []string{"vigie.src"})
	if res.OK || len(res.Diagnostics) == 0 {
		t.Fatalf("expected a diagnostic, got ok=%v diags=%v", res.OK, res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != "type-mismatch" {
		t.Fatalf("code = %q, want type-mismatch", d.Code)
	}
	if d.Decl != "main" {
		t.Fatalf("decl = %q, want main", d.Decl)
	}
	if d.Line != 1 {
		t.Fatalf("line = %d, want 1 (main's start line)", d.Line)
	}
	if !strings.Contains(d.Message, "'steps'") {
		t.Fatalf("message does not name the identifier: %q", d.Message)
	}
}

// A mismatch with no named identifier on either side (e.g. a literal vs a
// literal) must pass through unchanged — annotation is best-effort, never noise.
func TestMismatchWithoutIdentifierUnchanged(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { x := []int{1} x[0] = "s" println(x[0]) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil || !strings.Contains(cerr.Error(), "type mismatch") {
		t.Fatalf("expected a type mismatch error, got %v", cerr)
	}
}
