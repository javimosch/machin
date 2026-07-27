package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// arena_escape_test.go — escape(): carry ONE value out of an `arena { }` block.
//
// A scoped arena reclaims everything allocated inside it, which makes it unusable
// for the dominant server shape: do heavy transient work, return one answer. Before
// escape(), a handler either kept its garbage forever (single-actor loop, arena only
// freed when the goroutine returns — it never does) or reached for the UNCHECKED
// arena_reset(), which trusts the author to have dropped every live reference.
// escape(x) copies x into the ENCLOSING arena before the block's chain is freed, so
// the survivor has the caller's lifetime and ARENA001 can keep proving the rest.

func buildRunEscape(t *testing.T, src string) string {
	t.Helper()
	prog, err := ParseProgram([]string{normalize(src)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-escape-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return string(out)
}

// A value escaped from the block must still read correctly afterwards — the whole
// point, and the case that segfaults or yields garbage if the copy is wrong.
func TestEscapeStringSurvivesBlock(t *testing.T) {
	out := buildRunEscape(t, `func main() {
		answer := ""
		arena {
			s := "row-" + str(41 + 1)
			s = s + "/" + str(len(s))
			answer = escape(s)
		}
		println(answer)
		println(len(answer))
	}`)
	if got := strings.TrimSpace(out); got != "row-42/6\n8" {
		t.Fatalf("escaped string wrong after the block: %q", got)
	}
}

// Escaping inside a loop is the request-handler shape: every iteration's garbage
// dies, every answer survives.
func TestEscapeInLoopKeepsEveryAnswer(t *testing.T) {
	out := buildRunEscape(t, `func main() {
		acc := ""
		n := 0
		while n < 3 {
			ans := ""
			arena {
				big := ""
				i := 0
				while i < 50 { big = big + str(i)  i = i + 1 }
				ans = escape("n=" + str(n) + ":" + str(len(big)))
			}
			acc = acc + ans + " "
			n = n + 1
		}
		println(acc)
	}`)
	if got := strings.TrimSpace(out); got != "n=0:90 n=1:90 n=2:90" {
		t.Fatalf("per-iteration answers wrong: %q", got)
	}
}

// bytes carry their own buffer: the copy must move the payload, not just the header.
func TestEscapeBytesSurvivesBlock(t *testing.T) {
	out := buildRunEscape(t, `func main() {
		b := bytes("")
		arena {
			raw := from_hex("deadbeef")
			raw = bytes_concat(raw, bytes("!"))
			b = escape(raw)
		}
		println(to_hex(b))
		println(len(b))
	}`)
	if got := strings.TrimSpace(out); got != "deadbeef21\n5" {
		t.Fatalf("escaped bytes wrong after the block: %q", got)
	}
}

// Nested blocks must escape into the IMMEDIATELY enclosing arena, not the root:
// escaping to the root would leak (the outer block could never reclaim it).
func TestEscapeNestedBlocksHopOutOneLevel(t *testing.T) {
	out := buildRunEscape(t, `func main() {
		outer := ""
		arena {
			inner := ""
			arena {
				s := "deep-" + str(7)
				inner = escape(s)
			}
			outer = escape(inner + "-out")
		}
		println(outer)
	}`)
	if got := strings.TrimSpace(out); got != "deep-7-out" {
		t.Fatalf("nested escape wrong: %q", got)
	}
}

// ARENA001 must stay quiet on an escaped assignment (it provably does not dangle)
// while still firing on the same assignment without escape() — the analysis keeps
// its teeth, escape() is the one sanctioned exit.
func TestEscapeSatisfiesArenaEscapeAnalysis(t *testing.T) {
	withEscape := `func main() { answer := "" arena { s := "x" + str(1) answer = escape(s) } println(answer) }`
	without := `func main() { answer := "" arena { s := "x" + str(1) answer = s } println(answer) }`

	if fs := arenaEscapes(t, withEscape); len(fs) != 0 {
		t.Fatalf("escape() should silence ARENA001, got %+v", fs)
	}
	if fs := arenaEscapes(t, without); !hasEscapeIn(fs, "main") {
		t.Fatalf("ARENA001 must still fire on a non-escaped assignment out of an arena block, got %+v", fs)
	}
}

// escape() outside a block has nothing to copy out of: that is an author error,
// not a silent no-op.
func TestEscapeOutsideArenaBlockIsAnError(t *testing.T) {
	prog, err := ParseProgram([]string{normalize(`func main() { s := escape("x") println(s) }`)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-escape-err-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	err = BuildBinary(prog, bin.Name(), false)
	if err == nil {
		t.Fatal("escape() outside an arena block must not compile")
	}
	if !strings.Contains(err.Error(), "only valid inside an arena") {
		t.Fatalf("unhelpful error for escape() outside a block: %v", err)
	}
}

// An aggregate has no v1 copy: say so at compile time instead of copying a header
// whose backing store is about to be freed.
func TestEscapeUnsupportedTypeIsRejected(t *testing.T) {
	prog, err := ParseProgram([]string{normalize(`func main() { out := []string{} arena { xs := []string{"a", "b"} out = escape(xs) } println(len(out)) }`)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-escape-agg-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	err = BuildBinary(prog, bin.Name(), false)
	if err == nil {
		t.Fatal("escape() of a slice must not compile in v1 (its backing store would dangle)")
	}
	if !strings.Contains(err.Error(), "v1 supports") {
		t.Fatalf("unhelpful error for escaping an aggregate: %v", err)
	}
}

// The point of the feature: heavy transient work per iteration must not accumulate
// just because one small answer survives. Same floor rationale as
// TestScopedArenaReclaimsInlineAllocations — ru_maxrss carries a large fixed base,
// so assert a conservative ratio that still collapses on a real regression.
func TestEscapeKeepsReclamationEffective(t *testing.T) {
	body := func(scoped bool) string {
		work := `big := "" i := 0 while i < 500 { big = big + "row-" + str(i) i = i + 1 } ans = `
		if scoped {
			work = `arena { ` + work + `escape("len=" + str(len(big))) }`
		} else {
			work = work + `"len=" + str(len(big))`
		}
		return `func main() { last := "" n := 0 while n < 300 { ans := "" ` + work + ` last = ans n = n + 1 } println(last) }`
	}
	scopedOut, scopedRSS := buildRun(t, body(true))
	unscopedOut, unscopedRSS := buildRun(t, body(false))

	if scopedOut != unscopedOut {
		t.Fatalf("escape() changed program output: %q vs %q", scopedOut, unscopedOut)
	}
	if scopedRSS*3 > unscopedRSS {
		t.Fatalf("escape() lost reclamation: scoped %d kB vs unscoped %d kB (want scoped < 1/3)", scopedRSS, unscopedRSS)
	}
}

// A map made while a scoped arena is current must be reclaimed with it — before
// this, every transient map (a per-request lookup table, a per-page group) was a
// permanent leak, because maps were malloc-only with no free path anywhere.
func TestScopedArenaReclaimsMaps(t *testing.T) {
	prog := func(scoped bool) string {
		inner := `m := make(map[string]string) i := 0 while i < 400 { m["key-" + str(i)] = "value-" + str(i) i = i + 1 } total = total + len(keys(m))`
		if scoped {
			inner = `arena { ` + inner + ` }`
		}
		return `func main() { total := 0 n := 0 while n < 300 { ` + inner + ` n = n + 1 } println(total) }`
	}
	scopedOut, scopedRSS := buildRun(t, prog(true))
	unscopedOut, unscopedRSS := buildRun(t, prog(false))
	if scopedOut != unscopedOut {
		t.Fatalf("scoping changed output: %q vs %q", scopedOut, unscopedOut)
	}
	if scopedRSS*3 > unscopedRSS {
		t.Fatalf("maps not reclaimed with the arena: scoped %d kB vs unscoped %d kB", scopedRSS, unscopedRSS)
	}
}

// A long-lived map (made on the main arena) keeps the old malloc + per-entry free
// semantics, so delete-heavy caches still hand memory back and reads stay correct.
func TestMainArenaMapKeepsFreeingOnDelete(t *testing.T) {
	out := buildRunEscape(t, `func main() {
		m := make(map[string]int)
		i := 0
		while i < 2000 { m["k" + str(i)] = i  i = i + 1 }
		j := 0
		while j < 1500 { delete(m, "k" + str(j))  j = j + 1 }
		println(len(keys(m)))
		println(m["k1999"])
		println(has(m, "k0"))
	}`)
	if got := strings.TrimSpace(out); got != "500\n1999\nfalse" {
		t.Fatalf("long-lived map semantics changed: %q", got)
	}
}
