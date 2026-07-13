package main

import "testing"

// Issue #426: an `arena { }` block must reclaim allocations made *inline* in the
// block body — not only those made inside functions it calls. The existing
// TestScopedArenaBoundsMemory covers the called-function case (the work lives in
// work(i)); this pins the harder inline case, where the string building happens
// directly in the loop body under the arena.
//
// The unscoped loop retains every per-iteration string on the goroutine arena,
// so RSS climbs with the iteration count; wrapping the identical work in
// `arena { }` frees it each iteration, so RSS stays flat. Output must be
// byte-identical either way. Measured on the current tree: ~1.4 MB scoped vs
// ~95 MB unscoped over 400k iterations.
func TestScopedArenaReclaimsInlineAllocations(t *testing.T) {
	loop := func(scoped bool) string {
		// Build two fresh strings per iteration and fold their length into an
		// accumulator so the compiler can't drop the allocation as dead.
		inner := `s := "iter-" + str(n) s = s + "-" + str(n * n) total = total + len(s)`
		if scoped {
			inner = `arena { ` + inner + ` }`
		}
		return `func main() { total := 0 n := 0 while n < 400000 { ` + inner + ` n = n + 1 } println(total) }`
	}

	unscopedOut, unscopedRSS := buildRun(t, loop(false))
	scopedOut, scopedRSS := buildRun(t, loop(true))

	if scopedOut != unscopedOut {
		t.Fatalf("arena changed program output: scoped %q vs unscoped %q", scopedOut, unscopedOut)
	}
	// The unscoped loop retains ~all inline allocations; the scoped one frees
	// each iteration. Require at least a 5x reduction (the real gap is ~65x).
	// If inline allocations ever stop being reclaimed (#426), this regresses to
	// roughly parity and trips the guard.
	if scopedRSS*5 >= unscopedRSS {
		t.Fatalf("arena did not reclaim inline allocations: scoped RSS %d KB vs unscoped %d KB", scopedRSS, unscopedRSS)
	}
}
