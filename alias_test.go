package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// Unaliased-slice analysis (#578 step 1).
//
// Two properties are being pinned, and they pull against each other:
//
//   SOUND  — a slice with a live alias across an append must never qualify, or
//            the optimization this feeds would grow an array out from under a
//            dangling reference.
//   USEFUL — a slice aliased only AFTER its last append must qualify. A first,
//            flow-insensitive version was sound but fired on almost nothing real:
//            "build it, then use it" is the dominant shape, and it disqualified
//            every instance of it.

func aliasOf(t *testing.T, src string) map[string]SliceAlias {
	t.Helper()
	prog := progFromSrc(t, src)
	liftClosures(prog)
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	out := map[string]SliceAlias{}
	for _, s := range analyzeSliceAliasing(prog, c) {
		out[s.Var] = s // test programs use distinct variable names
	}
	return out
}

func mustQualify(t *testing.T, got map[string]SliceAlias, name string) {
	t.Helper()
	s, ok := got[name]
	if !ok {
		t.Fatalf("%s missing from the report: %+v", name, got)
	}
	if !s.Unaliased {
		t.Fatalf("%s should qualify, got refused: %s", name, s.Reason)
	}
}

func mustRefuse(t *testing.T, got map[string]SliceAlias, name string) {
	t.Helper()
	s, ok := got[name]
	if !ok {
		t.Fatalf("%s missing from the report: %+v", name, got)
	}
	if s.Unaliased {
		t.Fatalf("%s must be refused — a live alias crosses an append", name)
	}
	if s.Reason == "" {
		t.Fatalf("%s was refused without a reason", name)
	}
}

// The shape the optimization exists for: build in a loop, index it, never share.
func TestAliasSieveShapeQualifies(t *testing.T) {
	got := aliasOf(t, `
func main() {
    sieve := []int{}
    i := 0
    while i < 10 { sieve = append(sieve, 1)  i = i + 1 }
    sieve[0] = 0
    total := 0
    k := 0
    while k < len(sieve) { total = total + sieve[k]  k = k + 1 }
    println(total)
}`)
	mustQualify(t, got, "sieve")
	if got["sieve"].Appends != 1 {
		t.Fatalf("expected 1 append site, got %d", got["sieve"].Appends)
	}
}

// SOUNDNESS. Each of these creates the alias BEFORE an append, so growing in
// place would move the array out from under the alias.
func TestAliasBeforeAppendIsRefused(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"copy", `a := []int{1}
    b := a
    a = append(a, 2)
    println(b[0])`},
		{"param", `a := []int{1}
    println(helper(a))
    a = append(a, 2)
    println(a[0])`},
		{"closure", `a := []int{1}
    f := func() { return len(a) }
    a = append(a, 2)
    println(f())`},
		{"struct_field", `a := []int{1}
    b := Box{xs: a}
    a = append(a, 2)
    println(len(b.xs))`},
		{"return", `a := []int{1}
    if len(a) > 99 { return a }
    a = append(a, 2)
    println(a[0])`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aliasOf(t, `
type Box struct { xs []int }

func helper(xs) (r) { r = len(xs) }

func main() {
    `+tc.body+`
}`)
			mustRefuse(t, got, "a")
		})
	}
}

// An alias and an append inside the SAME loop cannot be ordered — the alias from
// one iteration is live across the next iteration's append.
func TestAliasAndAppendInSameLoopIsRefused(t *testing.T) {
	got := aliasOf(t, `
func main() {
    a := []int{1}
    keep := []int{}
    i := 0
    while i < 5 {
        keep = a
        a = append(a, i)
        i = i + 1
    }
    println(keep[0])
}`)
	mustRefuse(t, got, "a")
}

// USEFULNESS. The alias is created after the last append, so no append can move
// the array while the alias is live. A flow-insensitive analysis refuses these,
// which is what made it fire on essentially no real code.
func TestAliasAfterLastAppendStillQualifies(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"passed_to_a_call", `a := []int{}
    a = append(a, 1)
    a = append(a, 2)
    println(helper(a))`},
		{"copied", `a := []int{}
    a = append(a, 1)
    b := a
    println(b[0])`},
		{"stored_in_a_struct", `a := []int{}
    a = append(a, 1)
    b := Box{xs: a}
    println(len(b.xs))`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aliasOf(t, `
type Box struct { xs []int }

func helper(xs) (r) { r = len(xs) }

func main() {
    `+tc.body+`
}`)
			mustQualify(t, got, "a")
		})
	}
}

// A parameter is aliased by the caller before the body even starts, so no append
// in this frame can be safe.
func TestAliasParameterNeverQualifies(t *testing.T) {
	got := aliasOf(t, `
func grow(xs) (r) {
    xs = append(xs, 1)
    r = len(xs)
}

func main() {
    a := []int{1}
    println(grow(a))
}`)
	mustRefuse(t, got, "xs")
}

// Reads that cannot leak a reference must not disqualify: indexing, len, range,
// and writing through an index.
func TestAliasReadFormsDoNotDisqualify(t *testing.T) {
	got := aliasOf(t, `
func main() {
    a := []int{}
    a = append(a, 1)
    a[0] = 7
    n := a[0] + len(a)
    for _, v := range a { n = n + v }
    println(n)
}`)
	mustQualify(t, got, "a")
}

// A slice never appended to has nothing to optimize; it should still be reported,
// with an append count of zero, so the report cannot be mistaken for a win.
func TestAliasReportsZeroAppendSlices(t *testing.T) {
	got := aliasOf(t, `
func main() {
    a := []int{1, 2, 3}
    println(a[0])
}`)
	s, ok := got["a"]
	if !ok {
		t.Fatal("a missing from the report")
	}
	if s.Appends != 0 {
		t.Fatalf("expected 0 append sites, got %d", s.Appends)
	}
}

// The CLI wrapper: both output modes and the error path. `machin alias` is how a
// reviewer actually looks at this analysis, so it is worth covering rather than
// leaving as untested glue.
func captureAliasStdout(t *testing.T, args []string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	cmdErr := cmdAlias(args)
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), cmdErr
}

func TestAliasCommandJSONAndText(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "a.src", `func main() {
    xs := []int{}
    i := 0
    while i < 3 { xs = append(xs, i)  i = i + 1 }
    println(xs[0])
}`)

	out, err := captureAliasStdout(t, []string{"--json", f})
	if err != nil {
		t.Fatalf("alias --json: %v", err)
	}
	var rep AliasReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if !rep.OK || rep.Unaliased != 1 || rep.Total != 1 {
		t.Fatalf("want 1 of 1 unaliased, got %+v", rep)
	}
	if len(rep.Slices) != 1 || rep.Slices[0].Var != "xs" || !rep.Slices[0].Unaliased {
		t.Fatalf("unexpected slice report: %+v", rep.Slices)
	}

	txt, err := captureAliasStdout(t, []string{f})
	if err != nil {
		t.Fatalf("alias (text): %v", err)
	}
	for _, want := range []string{"xs", "append site", "provably unaliased"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("text output missing %q:\n%s", want, txt)
		}
	}
	// The report must say it is only a report — it is easy to mistake for a
	// shipped optimization otherwise.
	if !strings.Contains(txt, "no codegen consumes this yet") {
		t.Fatalf("text output should state that nothing consumes it:\n%s", txt)
	}
}

func TestAliasCommandNeedsAFile(t *testing.T) {
	if _, err := captureAliasStdout(t, []string{"--json"}); err == nil {
		t.Fatal("expected an error when no source file is given")
	}
}

func TestAliasCommandReportsRefusals(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "b.src", `func main() {
    a := []int{1}
    b := a
    a = append(a, 2)
    println(b[0])
}`)
	out, err := captureAliasStdout(t, []string{"--json", f})
	if err != nil {
		t.Fatalf("alias: %v", err)
	}
	var rep AliasReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Unaliased != 0 {
		t.Fatalf("an alias before an append must not qualify: %+v", rep)
	}
	found := false
	for _, s := range rep.Slices {
		if s.Var == "a" && !s.Unaliased && s.Reason != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected `a` refused with a reason: %+v", rep.Slices)
	}
}
