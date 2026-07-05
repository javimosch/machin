package main

import "testing"

// TestDeclName covers declName's three declaration shapes: plain, exported,
// and a decl with no ident (which must fall back to "").
func TestDeclName(t *testing.T) {
	cases := []struct{ decl, want string }{
		{`func add(a int, b int) int { return a + b }`, "add"},
		{`export func mul(a int, b int) int { return a * b }`, "mul"},
		{`type Point struct { x int y int }`, "Point"},
		{`var count int`, "count"},
		{``, ""},
	}
	for _, c := range cases {
		if got := declName(c.decl); got != c.want {
			t.Errorf("declName(%q): got %q, want %q", c.decl, got, c.want)
		}
	}
}

// TestFirstMeaningfulLine covers blank/comment-line skipping and the
// long-line truncation branch.
func TestFirstMeaningfulLine(t *testing.T) {
	got := firstMeaningfulLine("\n  \n// a comment\nfunc main() {}\n")
	if want := "func main() {}"; got != want {
		t.Errorf("firstMeaningfulLine: got %q, want %q", got, want)
	}

	if got := firstMeaningfulLine("\n  \n// only comments\n"); got != "" {
		t.Errorf("firstMeaningfulLine on all-blank/comment input: got %q, want \"\"", got)
	}

	long := ""
	for i := 0; i < 130; i++ {
		long += "x"
	}
	got = firstMeaningfulLine(long)
	if len(got) != 120 {
		t.Errorf("firstMeaningfulLine truncation: got len %d, want 120", len(got))
	}
	if got[117:] != "..." {
		t.Errorf("firstMeaningfulLine truncation: got suffix %q, want \"...\"", got[117:])
	}
}

// TestSeedStructNames covers both type and cstruct declarations, and that
// unrelated declarations don't seed anything.
func TestSeedStructNames(t *testing.T) {
	structs := map[string]bool{}
	seedStructNames(`type Point struct { x int y int }`, structs)
	seedStructNames(`cstruct div_t { quot i32 rem i32 }`, structs)
	seedStructNames(`func add(a int, b int) int { return a + b }`, structs)

	if !structs["Point"] {
		t.Error("seedStructNames: expected \"Point\" to be seeded from a type decl")
	}
	if !structs["div_t"] {
		t.Error("seedStructNames: expected \"div_t\" to be seeded from a cstruct decl")
	}
	if len(structs) != 2 {
		t.Errorf("seedStructNames: got %d entries, want 2 (func decl must not seed anything): %v", len(structs), structs)
	}
}

// TestCheckCstructLiteral is a regression test for a bug where seedStructNames
// required `cstruct` to be lexed as TKeyword, which it never is (it's only a
// soft keyword recognized inside `extern` blocks — see isExternKeyword in
// parser.go). That made `machin check` falsely report a parse error for any
// program constructing a cstruct literal (e.g. `div_t{...}`), even though the
// same program built fine via the real ParseProgram path.
func TestCheckCstructLiteral(t *testing.T) {
	src := `
extern "c" {
    header "stdlib.h"
    cstruct div_t { quot i32 rem i32 }
    fn div(i32, i32) div_t
}

func main() {
    d := div_t{quot: 1, rem: 2}
    println(d.quot)
}
`
	res := analyzeSource(src, []string{"probe.mfl"})
	if !res.OK {
		t.Fatalf("analyzeSource on a valid cstruct-literal program: got errors %+v, want OK", res.Diagnostics)
	}
}

// TestLocateCheckError covers the match and no-match paths: a message quoting
// a declared name resolves to that decl's line, otherwise ("", 0).
func TestLocateCheckError(t *testing.T) {
	decls := []string{
		`func add(a int, b int) int { return a + b }`,
		`func mul(a int, b int) int { return a * b }`,
	}
	lines := []int{10, 20}

	name, line := locateCheckError(`function "mul" has a type error`, decls, lines)
	if name != "mul" || line != 20 {
		t.Errorf("locateCheckError: got (%q, %d), want (\"mul\", 20)", name, line)
	}

	name, line = locateCheckError(`function "unknown" has a type error`, decls, lines)
	if name != "" || line != 0 {
		t.Errorf("locateCheckError with no matching decl: got (%q, %d), want (\"\", 0)", name, line)
	}
}
