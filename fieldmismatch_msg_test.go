package main

import (
	"strings"
	"testing"
)

// A type mismatch resolved through a STRUCT FIELD used to report
//
//	type mismatch: float vs int
//
// with no file, no line, no name, in a compiler that annotates local-variable
// mismatches beautifully ("type mismatch for 'kk' in \"main\": num vs struct").
// The reason is that field uses are deferred: `resolveDeferred` unions the
// field-use slot with the field's declared type long after the statement that
// produced it, and that path never called the annotator.
//
// It matters most in exactly the case where it said least — a large program,
// where there is no small region to scan and the only recourse is to bisect by
// deleting code until the message goes away.

// TestFieldMismatchNamesFieldAndStruct is the reported repro reduced: `best` is
// inferred float from `0.0 - 1` and then assigned an int, and the collision
// surfaces when the result is stored into an int field.
func TestFieldMismatchNamesFieldAndStruct(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type P struct { x float  st int }`,
		`func look(f, e) (best) {
			best = 0.0 - 1
			j := 0
			while j < 3 { if e.x > f.x { best = j } j = j + 1 }
		}`,
		`func main() { a := P{1.0, 0} b := P{2.0, 0} a.st = look(a, b) println(str(a.st)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil {
		t.Fatal("expected a type mismatch")
	}
	msg := err.Error()
	for _, want := range []string{"field 'st'", "of P", "float vs int"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not mention %q", msg, want)
		}
	}
}

// TestFieldMismatchNamesTheSource: naming the field says what was expected;
// naming the variable that supplied the value says where it came from. Between
// them there is nothing left to bisect.
func TestFieldMismatchNamesTheSource(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type P struct { st int }`,
		`func look() (best) { best = 1.5 }`,
		`func main() { a := P{0} a.st = look() println(str(a.st)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil {
		t.Fatal("expected a type mismatch")
	}
	msg := err.Error()
	if !strings.Contains(msg, "'best'") {
		t.Fatalf("message %q does not name the value's source", msg)
	}
	if !strings.Contains(msg, `"main"`) {
		t.Fatalf("message %q does not say where the assignment is", msg)
	}
	if strings.Contains(msg, "$") {
		t.Fatalf("message %q leaks a specialization suffix", msg)
	}
}

// TestFieldMismatchOnRead: reading a field into an incompatible variable is
// annotated too, not only assignment into one.
func TestFieldMismatchOnRead(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type P struct { st int }`,
		`func main() { a := P{1} s := "x" s = a.st println(s) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil {
		t.Fatal("expected a type mismatch")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// TestFieldAssignValidStillChecks: the ordinary path is untouched.
func TestFieldAssignValidStillChecks(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type P struct { st int }`,
		`func look() (best) { best = 3 }`,
		`func main() { a := P{0} a.st = look() println(str(a.st)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestLocalMismatchAnnotationUnchanged guards the message this one was modelled
// on: local-variable mismatches already named the variable and the function,
// and that must keep working.
func TestLocalMismatchAnnotationUnchanged(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { v := 1 v = "two" println(v) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "type mismatch for") {
		t.Fatalf("local annotation changed: %v", err)
	}
}

// TestMissingFieldErrorUnchanged: the neighbouring error in the same loop must
// not have been disturbed.
func TestMissingFieldErrorUnchanged(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type P struct { st int }`,
		`func main() { a := P{1} println(str(a.nope)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "has no field") {
		t.Fatalf("unexpected message: %v", err)
	}
}
