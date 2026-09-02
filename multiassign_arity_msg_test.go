package main

import (
	"strings"
	"testing"
)

// Using a one-value function as if it returned two used to report
//
//	2 variables but a single value on the right
//
// which names neither the callee, its arity, nor the function the mistake is
// in — so in a large program every multi-assignment in the build is a
// candidate and the only way to find it is to bisect by deleting code.
//
// The mirror-image mistake has always produced an excellent message
// ("grab returns 2 values; use a multi-assignment (a, b := grab(...))"), and
// the compiler has exactly the same information in hand for this direction.
// These tests pin the improved wording so it cannot quietly regress to the
// anonymous form.

// TestMultiAssignFromSingleReturnNamesCallee: the callee and its real arity
// appear in the message.
func TestMultiAssignFromSingleReturnNamesCallee(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func one(n) (v) { v = n + 1 }`,
		`func main() { a, b := one(3) println(str(a) + str(b)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil {
		t.Fatal("expected an arity error")
	}
	msg := err.Error()
	for _, want := range []string{"one returns 1 value", "2 variables on the left", `"main"`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not mention %q", msg, want)
		}
	}
}

// TestMultiAssignNamesEnclosingFunction: the mistake is reported against the
// function it is in, not the one being called, and without the "$N"
// specialization suffix that means nothing to a reader.
func TestMultiAssignNamesEnclosingFunction(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type S struct { k int }`,
		`func one(s) (v) { v = s.k }`,
		`func mid(s) (p, q) { p, q = one(s) }`,
		`func main() { s := S{1} a, b := mid(s) println(str(a + b)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil {
		t.Fatal("expected an arity error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"mid"`) {
		t.Fatalf("message %q does not name the enclosing function", msg)
	}
	if strings.Contains(msg, "$") {
		t.Fatalf("message %q leaks a specialization suffix", msg)
	}
}

// TestMultiAssignFromNonCall: a right-hand side that is not a call at all still
// gets a message that says so, rather than a bare count.
func TestMultiAssignFromNonCall(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { a, b := 3 + 4 println(str(a) + str(b)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil {
		t.Fatal("expected an arity error")
	}
	if !strings.Contains(err.Error(), "right-hand side is a single value") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// TestMultiAssignMirrorCaseUnchanged: the message this one was modelled on must
// keep working — it is the reference for the whole shape.
func TestMultiAssignMirrorCaseUnchanged(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type R struct { n int }`,
		`func grab(r) (v, w) { v = r.n  w = r.n + 1 }`,
		`func main() { r := R{1} c := grab(r) println(str(c)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "use a multi-assignment") {
		t.Fatalf("mirror-case message changed: %v", err)
	}
}

// TestMultiAssignWrongCountNamesBoth: a genuine 2-vs-3 mismatch keeps naming
// the callee and now also names where it happened.
func TestMultiAssignWrongCountNamesBoth(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func two() (a, b) { a = 1  b = 2 }`,
		`func main() { x, y, z := two() println(str(x + y + z)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil {
		t.Fatal("expected an arity error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "two returns 2 values but 3 are assigned") || !strings.Contains(msg, `"main"`) {
		t.Fatalf("unexpected message: %v", msg)
	}
}

// TestMultiAssignValidStillWorks: the ordinary case must be untouched.
func TestMultiAssignValidStillWorks(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func two() (a, b) { a = 1  b = 2 }`,
		`func main() { x, y := two() println(str(x + y)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}
