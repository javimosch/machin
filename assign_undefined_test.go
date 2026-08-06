package main

import (
	"strings"
	"testing"
)

// A plain `=` may only assign a variable that is already visible: a new local
// is introduced solely by `:=`. Assigning an undeclared name with `=` is almost
// always a typo (`cofnig = …` for `config`), so the checker rejects it — the
// same rule already enforced for the multi-assign form `a, b = …`.

// TestAssignUndefinedVarRejected verifies that `=` to a name that was never
// declared is a compile-time error rather than silently creating a local.
func TestAssignUndefinedVarRejected(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { x = 5 println(str(x)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "assignment to undefined variable") {
		t.Fatalf("expected undefined-variable error, got %v", err)
	}
}

// TestAssignUndefinedTypoRejected is the motivating footgun: a misspelled
// target name must not silently shadow the intended variable.
func TestAssignUndefinedTypoRejected(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { config := 10 cofnig = 20 println(str(config)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), `"cofnig"`) {
		t.Fatalf("expected error naming the misspelled variable, got %v", err)
	}
}

// TestAssignDeclaredVarOK confirms the ordinary reassignment path (declare with
// `:=`, then update with `=`) still type-checks.
func TestAssignDeclaredVarOK(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { a := 1 a = 2 println(str(a)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestAssignGlobalOK confirms `=` to a package global (never declared locally
// with `:=`) still assigns the global rather than erroring.
func TestAssignGlobalOK(t *testing.T) {
	prog, err := ParseProgram([]string{
		`var g = 0`,
		`func main() { g = 5 println(str(g)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestAssignFlatScopeReassignOK confirms flat function scope: a variable
// declared with `:=` inside a nested block may be updated with `=` after the
// block, because the name is already bound in the (flat) function scope by the
// time the `=` is checked.
func TestAssignFlatScopeReassignOK(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { if true { y := 1 println(str(y)) } y = 2 println(str(y)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestAssignBlankSingleDestOK confirms `_` is accepted as the sole assignment
// destination (not just inside a multi-assign): the value is discarded, never
// allocated as a local, and the assignment is never treated as undefined.
func TestAssignBlankSingleDestOK(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func f() (v) { v = 1 }`,
		`func main() { _ = f() println("ok") }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestAssignBlankSingleDestWalrusOK confirms `_ := f()` behaves the same as
// `_ = f()`: the blank identifier discards the value either way.
func TestAssignBlankSingleDestWalrusOK(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func f() (v) { v = 1 }`,
		`func main() { _ := f() println("ok") }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestAssignBlankUnreadableRejected confirms `_` remains write-only: reading
// it back (`x := _`) must still be a compile-time error.
func TestAssignBlankUnreadableRejected(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func f() (v) { v = 1 }`,
		`func main() { _ = f() x := _ println(str(x)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), `undefined variable "_"`) {
		t.Fatalf("expected undefined-variable error for reading _, got %v", err)
	}
}
