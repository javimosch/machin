package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// A closure LITERAL passed as a `go` call argument (#314) must survive the
// spawning goroutine's arena being reclaimed, same class of bug as #310's
// string/slice-argument hazards (see TestGoArgStringSurvivesSpawnerArena in
// mfl_test.go). MakeClosure previously allocated its captured-env struct with
// mfl_alloc, which is scoped to the CALLING goroutine's arena; once that arena
// is freed (the spawner returns) the closure's `env` pointer itself dangles,
// so dereferencing it later from the spawned goroutine reads corrupted/reused
// memory even though the captured value's own heap box (int/bool captures are
// calloc'd independently of any arena, see codegen.go function()) is fine.
// Fixed by allocating the env struct with plain malloc so its lifetime is
// independent of any arena (mirrors how channels/maps already manage their
// own memory). The capture here is a scalar (int) specifically to isolate
// this env-struct-lifetime bug from the second half of #314 -- a STRING
// captured by a closure remaining arena-owned bytes reachable through the
// box -- which is covered separately below by
// TestGoClosureCapturedDataSurvivesSpawnerArena.
// Looped many times since the corruption is timing-dependent -- a single run
// can pass by luck if the freed memory isn't reused in time.
func TestGoClosureArgSurvivesSpawnerArena(t *testing.T) {
	runLater := `func run_later(f, results) {
	sleep(15)
	results <- f()
}`
	spawnOne := `func spawn_one(n, results) {
	v := n * 1000 + n
	go run_later(func() { return v }, results)
}`
	churn := `func churn(n) {
	i := 0
	for i < 300 {
		s := "garbage-" + str(n) + "-" + str(i) + "-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
		if len(s) < 0 { println(s) }
		i = i + 1
	}
}`
	main := `func main() {
	results := make(chan int)
	i := 0
	for i < 25 {
		go spawn_one(i, results)
		go churn(i)
		i = i + 1
	}
	acc := ""
	j := 0
	for j < 25 { acc = acc + str(<-results) + "\n"  j = j + 1 }
	println(acc)
}`
	for round := 0; round < 2; round++ {
		out := runProg(t, runLater, spawnOne, churn, main)
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 25 {
			t.Fatalf("round %d: expected 25 lines, got %d:\n%s", round, len(lines), out)
		}
		want := map[string]bool{}
		for n := 0; n < 25; n++ {
			want[strconv.Itoa(n*1000+n)] = true
		}
		seen := map[string]bool{}
		for _, l := range lines {
			if !want[l] {
				t.Fatalf("round %d: corrupted closure capture (use-after-free, #314): %q", round, l)
			}
			if seen[l] {
				t.Fatalf("round %d: duplicate value %q (aliased/corrupted env):\n%s", round, l, out)
			}
			seen[l] = true
		}
		if len(seen) != 25 {
			t.Fatalf("round %d: expected 25 distinct values, got %d:\n%s", round, len(seen), out)
		}
	}
}

// The follow-on gap the scalar test above deliberately left out (#314, second
// half): a STRING (or struct-of-strings, or slice) captured by a closure
// literal passed directly to `go` had stable env+box pointers (the fix above)
// but the box still pointed at DATA in the spawning goroutine's arena. The
// spawner here is itself a short-lived goroutine -- the shape the original
// repro needed -- and churn goroutines pressure the allocator so freed arena
// memory is actually reused. goStmt now freeze/thaws the box contents through
// the env->box indirection (strings/structs by offset, slices/maps via the
// JSON round-trip), so every value must arrive intact and distinct.
func TestGoClosureCapturedDataSurvivesSpawnerArena(t *testing.T) {
	typ := `type Agent struct { name string  greeting string }`
	runLater := `func run_later(f) { sleep(15) f() }`
	spawnStr := `func spawn_str(n, results) {
	label := "conv-id-0123456789-" + str(n)
	go run_later(func() { results <- "s:" + label })
}`
	spawnStruct := `func spawn_struct(n, results) {
	ag := Agent{name: "assistant-padding-xxxxxxxxxxxxxxxxxxxxxxxx", greeting: "greet-" + str(n)}
	go run_later(func() { results <- "t:" + ag.greeting })
}`
	spawnSlice := `func spawn_slice(n, results) {
	tags := []string{"alpha-" + str(n), "beta-" + str(n)}
	go run_later(func() { results <- "l:" + tags[0] + "," + tags[1] })
}`
	churn := `func churn(n) {
	i := 0
	for i < 400 {
		s := "garbage-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-" + str(n) + "-" + str(i)
		if len(s) < 0 { println(s) }
		i = i + 1
	}
}`
	main := `func main() {
	results := make(chan string)
	i := 0
	for i < 10 {
		go spawn_str(i, results)
		go spawn_struct(i, results)
		go spawn_slice(i, results)
		go churn(i)
		i = i + 1
	}
	acc := ""
	j := 0
	for j < 30 { acc = acc + <-results + "\n" j = j + 1 }
	println(acc)
}`
	out := runProg(t, typ, runLater, spawnStr, spawnStruct, spawnSlice, churn, main)
	want := map[string]bool{}
	for i := 0; i < 10; i++ {
		want[fmt.Sprintf("s:conv-id-0123456789-%d", i)] = true
		want[fmt.Sprintf("t:greet-%d", i)] = true
		want[fmt.Sprintf("l:alpha-%d,beta-%d", i, i)] = true
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 30 {
		t.Fatalf("expected 30 lines, got %d:\n%s", len(lines), out)
	}
	seen := map[string]bool{}
	for _, l := range lines {
		if !want[l] {
			t.Fatalf("corrupted captured data (use-after-free, #314): %q", l)
		}
		if seen[l] {
			t.Fatalf("duplicate value %q (aliased/corrupted capture):\n%s", l, out)
		}
		seen[l] = true
	}
}
