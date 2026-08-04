package main

import "testing"

// Issue #546: a parameter sharing a name with its own function's named return
// used to alias the same env slot in the checker (types.go instantiate), which
// downstream became two conflicting C declarations of the same identifier —
// cc rejected it with a raw symbol-collision error instead of a clean MFL
// diagnostic. Check now rejects this at the type-checking stage, for any type.

func TestParamNamedReturnCollision(t *testing.T) {
	checkErr(t, `parameter "n" has the same name as a named return value`,
		`func bump(n) (n) { n = n + 1 }`,
		`func main() { r := bump(5) println(r) }`,
	)
}

func TestParamNamedReturnCollisionString(t *testing.T) {
	checkErr(t, `parameter "s" has the same name as a named return value`,
		`func shout(s) (s) { s = s + "!" }`,
		`func main() { r := shout("hi") println(r) }`,
	)
}

func TestParamNamedReturnNoCollision(t *testing.T) {
	got := runNative(t,
		`func bump(n) (out) { out = n + 1 }`,
		`func main() { r := bump(5) println(r) }`,
	)
	if got != "6\n" {
		t.Fatalf("distinct param/return names: got %q, want \"6\\n\"", got)
	}
}
