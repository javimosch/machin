package main

import "testing"

// TestMapDeleteNonexistentAndTwiceIsNoop covers mfl_map_del (codegen.go):
// deleting a key that was never present, and deleting the same key twice,
// must both be safe no-ops rather than freeing an already-freed entry.
func TestMapDeleteNonexistentAndTwiceIsNoop(t *testing.T) {
	got := runProg(t,
		`func main() { m := make(map[int]string) m[1] = "one" delete(m, 99) delete(m, 1) delete(m, 1) println(len(m), has(m, 1)) }`)
	if want := "0 false\n"; got != want {
		t.Fatalf("map delete no-op: got %q, want %q", got, want)
	}
}

// TestMapHasAfterDeleteThenReinsert covers mfl_map_has/mfl_map_del/mfl_map_set
// (codegen.go): has() must flip to false right after delete(), and the same
// key must be insertable again afterward.
func TestMapHasAfterDeleteThenReinsert(t *testing.T) {
	got := runProg(t,
		`func main() { m := make(map[string]int) m["a"] = 1 delete(m, "a") println(has(m, "a")) m["a"] = 2 println(has(m, "a"), m["a"]) }`)
	if want := "false\ntrue 2\n"; got != want {
		t.Fatalf("map has/delete/reinsert: got %q, want %q", got, want)
	}
}

// TestMapHasOnEmptyMap covers mfl_map_has (codegen.go) on a just-created,
// never-populated map.
func TestMapHasOnEmptyMap(t *testing.T) {
	got := runProg(t,
		`func main() { m := make(map[int]int) println(has(m, 0), len(m)) }`)
	if want := "false 0\n"; got != want {
		t.Fatalf("map has on empty map: got %q, want %q", got, want)
	}
}

// TestContainsEdgeCases covers mfl_contains (codegen.go, strstr-backed):
// an empty needle always matches, the whole string matches itself, an
// absent substring doesn't match, and matching is case-sensitive.
func TestContainsEdgeCases(t *testing.T) {
	got := runNative(t, `func main(){
		println(contains("hello", ""))
		println(contains("hello", "hello"))
		println(contains("hello", "zz"))
		println(contains("hello", "HELLO"))
	}`)
	if want := "true\ntrue\nfalse\nfalse\n"; got != want {
		t.Fatalf("contains edge cases: got %q, want %q", got, want)
	}
}
