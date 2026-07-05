package main

import (
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
// this env-struct-lifetime bug from the separate, still-open gap where a
// STRING captured by a closure remains arena-owned bytes reachable through
// the box (chanNeedsJSON/chanStrOffsets has no case for func-typed args to
// freeze/thaw those) -- a separate follow-on fix, out of scope here.
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
