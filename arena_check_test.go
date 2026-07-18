package main

import (
	"strings"
	"testing"
)

// arenaEscapes parses + checks a full program and returns the ARENA001 findings. Each fixture
// must include a `main` that reaches `f`, since the analysis runs over instantiated functions.
func arenaEscapes(t *testing.T, decls ...string) []arenaFinding {
	t.Helper()
	nd := make([]string, len(decls))
	for i, d := range decls {
		nd[i] = normalize(d)
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return detectArenaEscapes(prog, c)
}

func hasEscapeIn(fs []arenaFinding, fn string) bool {
	for _, f := range fs {
		if f.Decl == fn {
			return true
		}
	}
	return false
}

// TestArenaClosureCaptureEscape: a closure that captures an arena-tainted variable and then
// escapes the block dangles — the capture is read inside the block but stored in the closure's
// block-outliving environment, so calling it after reclamation reads freed memory. Both a direct
// escape and one propagated through an inner closure-valued local must fire.
func TestArenaClosureCaptureEscape(t *testing.T) {
	// direct: the escaping named return IS the capturing closure.
	direct := arenaEscapes(t,
		`func f() (r) { arena { s := "x" + str(1)  r = func() { println(s) } } }`,
		`func main() { g := f()  g() }`)
	if !hasEscapeIn(direct, "f") {
		t.Errorf("direct closure-capture escape: expected ARENA001, got %+v", direct)
	}
	// propagated: closure bound to an inner local, which then escapes.
	prop := arenaEscapes(t,
		`func f() (r) { arena { s := "x" + str(1)  g := func() { println(s) }  r = g } }`,
		`func main() { h := f()  h() }`)
	if !hasEscapeIn(prop, "f") {
		t.Errorf("propagated closure-capture escape: expected ARENA001, got %+v", prop)
	}
}

// TestArenaClosureCaptureClean: closures that cannot dangle must NOT fire — one capturing
// nothing, one capturing only a pre-existing parameter, and one that never leaves the block.
func TestArenaClosureCaptureClean(t *testing.T) {
	cases := []struct {
		name  string
		decls []string
	}{
		{"captureless", []string{
			`func f() (r) { arena { println("hi")  r = func() { println("static") } } }`,
			`func main() { g := f()  g() }`}},
		{"captures-only-param", []string{
			`func f(p) (r) { arena { r = func() { println(p) } } }`,
			`func main() { g := f("hi")  g() }`}},
		{"closure-stays-inner", []string{
			`func f() (r) { arena { s := "x" + str(1)  g := func() { println(s) }  g() }  r = "done" }`,
			`func main() { println(f()) }`}},
	}
	for _, tc := range cases {
		if fs := arenaEscapes(t, tc.decls...); hasEscapeIn(fs, "f") {
			t.Errorf("%s: expected NO escape, got %+v", tc.name, fs)
		}
	}
}

// TestArenaFieldGranularityEscape: assigning arena memory into an INNER container's field/element
// taints that place, so escaping the whole container is caught (a soundness fix — per-variable
// taint missed this because the container was built from a pre-existing value and never
// wholesale-assigned an arena value).
func TestArenaFieldGranularityEscape(t *testing.T) {
	cases := []struct {
		name  string
		decls []string
	}{
		{"field-assign-then-escape-struct", []string{
			`type Box struct { items []string }`,
			`func f(seed) (r) { arena { p := Box{seed}  p.items = append(p.items, "x" + str(1))  r = p } }`,
			`func main() { b := f([]string{})  println(str(len(b.items))) }`}},
		{"index-assign-then-escape-slice", []string{
			`func f(seed) (r) { arena { xs := seed  xs[0] = "a" + str(1)  r = xs } }`,
			`func main() { b := f([]string{"z"})  println(b[0]) }`}},
		{"extract-the-tainted-field", []string{
			`type Pair struct { a []string  b []string }`,
			`func f(s1, s2) (r) { arena { p := Pair{s1, s2}  p.a = append(p.a, "x" + str(1))  r = p.a } }`,
			`func main() { z := f([]string{}, []string{"ok"})  println(str(len(z))) }`}},
	}
	for _, tc := range cases {
		if fs := arenaEscapes(t, tc.decls...); !hasEscapeIn(fs, "f") {
			t.Errorf("%s: expected an ARENA001 escape, got none", tc.name)
		}
	}
}

// TestArenaFieldGranularityClean: after tainting ONE field of an inner struct, extracting a
// DIFFERENT (untouched) field must stay clean — per-place taint does not over-report the way
// whole-variable taint would.
func TestArenaFieldGranularityClean(t *testing.T) {
	fs := arenaEscapes(t,
		`type Pair struct { a []string  b []string }`,
		`func f(s1, s2) (r) { arena { p := Pair{s1, s2}  p.a = append(p.a, "x" + str(1))  r = p.b } }`,
		`func main() { z := f([]string{}, []string{"ok"})  println(z[0]) }`)
	if hasEscapeIn(fs, "f") {
		t.Errorf("extracting an untouched clean field must not fire, got %+v", fs)
	}
}

// TestArenaEscapeVectors: every way an arena-allocated value can outlive its block must fire.
func TestArenaEscapeVectors(t *testing.T) {
	cases := []struct {
		name  string
		decls []string
	}{
		{"named-return", []string{
			`func f() (r) { arena { s := "x" + str(1)  r = s } }`,
			`func main() { println(f()) }`}},
		{"outer-var", []string{
			`func f() (r) { x := ""  arena { x = "a" + str(2) }  r = x }`,
			`func main() { println(f()) }`}},
		{"bare-return", []string{
			`func f(n) (r) { arena { return "v" + str(n) }  r = "" }`,
			`func main() { println(f(1)) }`}},
		{"append-outer-slice", []string{
			`func f() (r) { xs := []int{}  arena { xs = append(xs, len("q")) }  r = str(len(xs)) }`,
			`func main() { println(f()) }`}},
		{"outer-slice-index", []string{
			`func f() (r) { xs := []string{"a"}  arena { xs[0] = "b" + str(3) }  r = xs[0] }`,
			`func main() { println(f()) }`}},
	}
	for _, tc := range cases {
		fs := arenaEscapes(t, tc.decls...)
		if !hasEscapeIn(fs, "f") {
			t.Errorf("%s: expected an ARENA001 escape, got none", tc.name)
		}
	}
}

// TestArenaEscapeStructField: storing an arena string into a field of a struct that outlives
// the block escapes.
func TestArenaEscapeStructField(t *testing.T) {
	fs := arenaEscapes(t,
		`type P struct { name string }`,
		`func f() (r) { p := P{"init"}  arena { p.name = "n" + str(1) }  r = p.name }`,
		`func main() { println(f()) }`,
	)
	if !hasEscapeIn(fs, "f") {
		t.Errorf("expected a struct-field escape, got %+v", fs)
	}
}

// TestArenaClean: legitimate arena patterns must not fire (no false positives — a clean result
// is the memory-safety proof, so over-reporting would break real programs).
func TestArenaClean(t *testing.T) {
	cases := []struct {
		name  string
		decls []string
	}{
		{"accumulator", []string{
			`func f(n) (r) { total := 0  arena { s := "i" + str(n)  total = total + len(s) }  r = total }`,
			`func main() { println(str(f(3))) }`}},
		{"param-out", []string{
			`func f(p) (r) { arena { r = p } }`,
			`func main() { println(f("hi")) }`}},
		{"all-inner", []string{
			`func f(n) (r) { arena { s := "x" + str(n)  t := s + "!"  println(t) }  r = "done" }`,
			`func main() { println(f(2)) }`}},
		{"scalar-field-out", []string{
			`type C struct { k int }`,
			`func f() (r) { total := 0  arena { c := C{5}  total = total + c.k }  r = total }`,
			`func main() { println(str(f())) }`}},
		{"inner-slice-reduce", []string{
			`func f(n) (r) { sum := 0  arena { xs := []int{n, n + 1}  for _, v := range xs { sum = sum + v } }  r = sum }`,
			`func main() { println(str(f(4))) }`}},
	}
	for _, tc := range cases {
		fs := arenaEscapes(t, tc.decls...)
		if hasEscapeIn(fs, "f") {
			t.Errorf("%s: expected NO escape, got %+v", tc.name, fs)
		}
	}
}

// TestArenaSendIsSafe: sending an arena value on a channel is NOT an escape — the runtime
// deep-copies values crossing a channel, so the receiver never sees the arena buffer.
func TestArenaSendIsSafe(t *testing.T) {
	fs := arenaEscapes(t,
		`func f(ch) { arena { ch <- "msg" + str(1) } }`,
		`func main() { ch := make(chan string)  go f(ch)  println(<-ch) }`,
	)
	if hasEscapeIn(fs, "f") {
		t.Errorf("a channel send must not be flagged (runtime copies), got %+v", fs)
	}
}

// TestArenaNoArenaNoFindings: a program with no arena block produces nothing.
func TestArenaNoArenaNoFindings(t *testing.T) {
	fs := arenaEscapes(t,
		`func f(a) (r) { r = a + 1 }`,
		`func main() { println(str(f(2))) }`,
	)
	if len(fs) != 0 {
		t.Errorf("no arena block should mean no findings, got %+v", fs)
	}
}

// TestArenaDiagnosticWiring: ARENA001 surfaces through the checker as an advisory warning
// (phase "arena"), never as an error.
func TestArenaDiagnosticWiring(t *testing.T) {
	res := analyzeSource("func f() (r) { arena { s := \"x\" + str(1)  r = s } }\nfunc main() { println(f()) }\n", []string{"t.mfl"})
	if !res.OK {
		t.Error("an arena escape is advisory; it must not make the check fail")
	}
	found := false
	for _, w := range res.Warnings {
		if w.Code == "ARENA001" && w.Phase == "arena" {
			found = true
			if !strings.Contains(w.Message, "dangles") {
				t.Errorf("message should explain the danger: %q", w.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected an ARENA001 warning, got %+v", res.Warnings)
	}
}

// hasCodeIn reports whether any finding for fn has the given code.
func hasCodeIn(fs []arenaFinding, fn, code string) bool {
	for _, f := range fs {
		if f.Decl == fn && f.Code == code {
			return true
		}
	}
	return false
}

// TestArenaReturnInside: ANY return inside an arena block is ARENA002 — the generated code
// returns before the block's cleanup, leaking it and dangling the current-arena pointer. This
// fires regardless of the returned value (a case the ARENA001 value-escape scan missed).
func TestArenaReturnInside(t *testing.T) {
	// return of a freshly-allocated value
	fs := arenaEscapes(t,
		`func f(n) (r) { arena { return "e" + str(n) }  r = "x" }`,
		`func main() { println(f(1)) }`,
	)
	if !hasCodeIn(fs, "f", "ARENA002") {
		t.Errorf("return of an allocated value inside arena should be ARENA002, got %+v", fs)
	}
	// bare return of nothing — still corrupts the current-arena pointer, so still ARENA002
	fs = arenaEscapes(t,
		`func f(n) (r) { r = 0  arena { if n > 0 { return }  r = 5 } }`,
		`func main() { println(str(f(1))) }`,
	)
	if !hasCodeIn(fs, "f", "ARENA002") {
		t.Errorf("a bare return inside arena should be ARENA002, got %+v", fs)
	}
	// return of a scalar — still skips cleanup, still ARENA002
	fs = arenaEscapes(t,
		`func f(n) (r) { arena { return n * 2 }  r = 0 }`,
		`func main() { println(str(f(3))) }`,
	)
	if !hasCodeIn(fs, "f", "ARENA002") {
		t.Errorf("a scalar return inside arena should be ARENA002, got %+v", fs)
	}
}

// TestArenaReturnAfterIsClean: returning AFTER the arena block is fine (cleanup already ran).
func TestArenaReturnAfterIsClean(t *testing.T) {
	fs := arenaEscapes(t,
		`func f(n) (r) { arena { s := "e" + str(n)  println(s) }  r = "done" }`,
		`func main() { println(f(1)) }`,
	)
	if hasCodeIn(fs, "f", "ARENA002") {
		t.Errorf("a return after the arena block must not be ARENA002, got %+v", fs)
	}
}

// TestArenaReturnInClosureNotFlagged: a return in a closure declared inside an arena belongs to
// the closure, not the arena's function — it must not be ARENA002.
func TestArenaReturnInClosureNotFlagged(t *testing.T) {
	fs := arenaEscapes(t,
		`func f() (r) { g := func() { return 1 }  arena { println(str(g())) }  r = 0 }`,
		`func main() { println(str(f())) }`,
	)
	if hasCodeIn(fs, "f", "ARENA002") {
		t.Errorf("a closure's own return must not be ARENA002, got %+v", fs)
	}
}
