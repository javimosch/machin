package main

import "testing"

// TestArenaInterprocPassthrough: a pass-through helper called inside an arena is NOT an escape
// when its argument is pre-existing — the v1 over-report this slice removes.
func TestArenaInterprocPassthrough(t *testing.T) {
	fs := arenaEscapes(t,
		`func id(s) (r) { r = s }`,
		`func f(p) (r) { x := ""  arena { x = id(p) }  r = x }`,
		`func main() { println(f("hi")) }`,
	)
	if hasEscapeIn(fs, "f") {
		t.Errorf("a pass-through helper of a pre-existing param must not escape, got %+v", fs)
	}
}

// TestArenaInterprocFresh: a helper that allocates fresh heap still escapes.
func TestArenaInterprocFresh(t *testing.T) {
	fs := arenaEscapes(t,
		`func wrap(s) (r) { r = "[" + s + "]" }`,
		`func f(p) (r) { x := ""  arena { x = wrap(p) }  r = x }`,
		`func main() { println(f("hi")) }`,
	)
	if !hasEscapeIn(fs, "f") {
		t.Errorf("a fresh-allocating helper must escape, got none")
	}
}

// TestArenaInterprocPassthroughTainted: pass-through composes — id(tainted) is still tainted.
func TestArenaInterprocPassthroughTainted(t *testing.T) {
	fs := arenaEscapes(t,
		`func id(s) (r) { r = s }`,
		`func f() (r) { x := ""  arena { t := "a" + str(1)  x = id(t) }  r = x }`,
		`func main() { println(f()) }`,
	)
	if !hasEscapeIn(fs, "f") {
		t.Errorf("pass-through of a tainted value must escape, got none")
	}
}

// TestArenaInterprocAliasChain: provenance traces through a local alias chain b := a; a := p.
func TestArenaInterprocAliasChain(t *testing.T) {
	// clean: chain ends at a pre-existing param
	clean := arenaEscapes(t,
		`func chain(p) (r) { a := p  b := a  r = b }`,
		`func f(p) (r) { x := ""  arena { x = chain(p) }  r = x }`,
		`func main() { println(f("hi")) }`,
	)
	if hasEscapeIn(clean, "f") {
		t.Errorf("alias chain of a param must not escape, got %+v", clean)
	}
	// escape: same shape but the chain starts from a fresh allocation
	esc := arenaEscapes(t,
		`func mk(p) (r) { a := p + "!"  b := a  r = b }`,
		`func f(p) (r) { x := ""  arena { x = mk(p) }  r = x }`,
		`func main() { println(f("hi")) }`,
	)
	if !hasEscapeIn(esc, "f") {
		t.Errorf("alias chain of a fresh value must escape, got none")
	}
}

// TestArenaInterprocRecursion: a (mutually) recursive helper must not hang the fixpoint and must
// be summarized soundly. A recursive pass-through stays clean; a recursive allocator escapes.
func TestArenaInterprocRecursion(t *testing.T) {
	// recursive pass-through: peel returns its argument (or a smaller slice of it) — no fresh alloc
	clean := arenaEscapes(t,
		`func pick(s, n) (r) { r = s  if n > 0 { r = pick(s, n - 1) } }`,
		`func f(p) (r) { x := ""  arena { x = pick(p, 3) }  r = x }`,
		`func main() { println(f("hi")) }`,
	)
	if hasEscapeIn(clean, "f") {
		t.Errorf("recursive pass-through must not escape, got %+v", clean)
	}
	// recursive allocator: build concatenates on every level
	esc := arenaEscapes(t,
		`func build(s, n) (r) { r = s  if n > 0 { r = s + build(s, n - 1) } }`,
		`func f(p) (r) { x := ""  arena { x = build(p, 3) }  r = x }`,
		`func main() { println(f("hi")) }`,
	)
	if !hasEscapeIn(esc, "f") {
		t.Errorf("recursive allocator must escape, got none")
	}
}

// TestArenaProvSummaryDirect exercises computeRetProv's classification directly.
func TestArenaProvSummaryDirect(t *testing.T) {
	nd := []string{
		normalize(`func id(s) (r) { r = s }`),
		normalize(`func wrap(s) (r) { r = "<" + s + ">" }`),
		// call both so they instantiate; an arena so computeRetProv runs
		normalize(`func f() (r) { arena { r = id("x") + wrap("y") } }`),
		normalize(`func main() { println(f()) }`),
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	sum := computeRetProv(prog, c)
	if sum["id"] == nil || sum["id"].fresh || !sum["id"].pass[0] {
		t.Errorf("id should be (fresh=false, pass={0}), got %+v", sum["id"])
	}
	if sum["wrap"] == nil || !sum["wrap"].fresh {
		t.Errorf("wrap should be fresh, got %+v", sum["wrap"])
	}
}
