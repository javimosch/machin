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
	// A collision on a function-local from disjoint `:=` branches must spell out
	// the function-scoping rule, since Go instincts expect two block-locals (#507).
	if !strings.Contains(msg, "does not shadow — variables are function-scoped") {
		t.Fatalf("message does not explain function scoping: %q", msg)
	}
}

// The function-scoping note is specific to locals: a collision on a package
// GLOBAL (which genuinely cannot shadow anything) must NOT claim it does.
func TestMismatchGlobalOmitsShadowNote(t *testing.T) {
	prog, err := ParseProgram([]string{
		`var steps = "a,b,c"`,
		`func main() { steps = split("a,b,c", ",") println(steps[0]) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil || !strings.Contains(cerr.Error(), "type mismatch") {
		t.Fatalf("expected a type mismatch error, got %v", cerr)
	}
	if strings.Contains(cerr.Error(), "does not shadow") {
		t.Fatalf("global mismatch should not mention shadowing: %q", cerr.Error())
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

// A parameter's type, fixed by inference from one call site, conflicting with a
// later call must NOT carry the `:= does not shadow` hint: no `:=` redeclaration
// is involved at all, so the hint would send the reader down the wrong path
// (issue #551, case "which call site fixed a parameter's type").
func TestMismatchParamConflictOmitsShadowNote(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func report(flag, name) (n) { n = 0 if flag == 1 { println(name) } }`,
		`func main() { x := report(1, "int call") y := report(2 == 2, "bool call") println(str(x + y)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil || !strings.Contains(cerr.Error(), "type mismatch") {
		t.Fatalf("expected a type mismatch error, got %v", cerr)
	}
	if !strings.Contains(cerr.Error(), "'flag'") {
		t.Fatalf("message does not name the offending parameter: %q", cerr.Error())
	}
	if strings.Contains(cerr.Error(), "does not shadow") {
		t.Fatalf("param-conflict mismatch has no := redeclaration, should not mention shadowing: %q", cerr.Error())
	}
}

// A builtin whose return type conflicts with a parameter's inferred type must
// also omit the shadow hint — the collision comes from `charat`'s return, not a
// `:=` redeclaration (issue #551, case "a builtin's return type").
func TestMismatchBuiltinReturnOmitsShadowNote(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func hash(h, s) (o) { o = h i := 0 while i < len(s) { o = o + charat(s, i) i = i + 1 } }`,
		`func main() { println(str(hash(7, "ab"))) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil || !strings.Contains(cerr.Error(), "type mismatch") {
		t.Fatalf("expected a type mismatch error, got %v", cerr)
	}
	if !strings.Contains(cerr.Error(), "'h'") {
		t.Fatalf("message does not name the offending identifier: %q", cerr.Error())
	}
	if strings.Contains(cerr.Error(), "does not shadow") {
		t.Fatalf("builtin-return mismatch has no := redeclaration, should not mention shadowing: %q", cerr.Error())
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

// A mismatch must name the EXPRESSION that produced the conflicting type, not
// only the variable that ended up with it (#551). The variable is the effect;
// the expression is the cause, and on a long function the cause is what you have
// to go find. No line number: MFL's canonical form is one declaration per line,
// so every expression in a function shares one.
func TestMismatchNamesTheConflictingBuiltin(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func hash(h, s) (o) { o = h i := 0 while i < len(s) { o = o + charat(s, i) i = i + 1 } }`,
		`func main() { println(str(hash(7, "ab"))) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil {
		t.Fatal("expected a type mismatch")
	}
	// charat returns a string, not a byte — that is the actual mistake, and the
	// message used to blame only the accumulator `h`.
	if !strings.Contains(cerr.Error(), "charat(s, i)") {
		t.Fatalf("message does not name the builtin whose return type conflicts: %q", cerr.Error())
	}
}

// The same for a parameter whose type was fixed by an earlier call: name the
// call that conflicts, so you do not bisect a dozen call sites by hand.
func TestMismatchNamesTheConflictingCall(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func report(flag, name) (n) { n = 0 if flag == 1 { println(name) } }`,
		`func main() { x := report(1, "int call") y := report(2 == 2, "bool call") println(str(x + y)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil {
		t.Fatal("expected a type mismatch")
	}
	if !strings.Contains(cerr.Error(), "report(2 == 2") {
		t.Fatalf("message does not name the conflicting call: %q", cerr.Error())
	}
}

// An assignment is caused by its right-hand side.
func TestMismatchNamesTheAssignedExpression(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { steps := "a,b,c" steps = split("a,b,c", ",") println(steps[0]) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil {
		t.Fatal("expected a type mismatch")
	}
	if !strings.Contains(cerr.Error(), `split("a,b,c", ",")`) {
		t.Fatalf("message does not name the assigned expression: %q", cerr.Error())
	}
}

// A bare identifier or literal adds nothing the message does not already say, so
// it must NOT be appended — the cause is only worth printing when it is a real
// expression.
func TestMismatchOmitsNoisyCause(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { v := 1 v = "s" println(v) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, cerr := Check(prog)
	if cerr == nil {
		t.Fatal("expected a type mismatch")
	}
	if strings.Contains(cerr.Error(), "— from") {
		t.Fatalf("a literal cause should be omitted as noise: %q", cerr.Error())
	}
}
