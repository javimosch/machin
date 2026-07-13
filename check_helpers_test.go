package main

import (
	"strings"
	"testing"
)

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

// TestFirstMeaningfulLine covers leading blank/comment lines being skipped, a
// normal line returned as-is, long-line truncation past 120 chars (with a "..."
// suffix), and an all-blank/all-comment block falling through to "".
func TestFirstMeaningfulLine(t *testing.T) {
	if got := firstMeaningfulLine("\n// a comment\n\n    x := 1\n"); got != "x := 1" {
		t.Errorf("firstMeaningfulLine: got %q, want %q", got, "x := 1")
	}

	long := ""
	for len(long) < 130 {
		long += "a"
	}
	got := firstMeaningfulLine(long)
	if len(got) != 120 || !strings.HasSuffix(got, "...") {
		t.Errorf("firstMeaningfulLine on a %d-char line: got len %d (%q), want len 120 ending in \"...\"", len(long), len(got), got)
	}
	if got[:117] != long[:117] {
		t.Errorf("firstMeaningfulLine truncation: got prefix %q, want %q", got[:117], long[:117])
	}

	if got := firstMeaningfulLine("\n// only comments\n   \n// more\n"); got != "" {
		t.Errorf("firstMeaningfulLine on an all-blank/comment block: got %q, want \"\"", got)
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

// TestSplitFunctionsLoc covers splitting multiple decls with leading blank/comment
// lines (skipped from the start-line count), a brace inside a string literal (must
// not affect depth tracking), and the unbalanced-braces error path.
func TestSplitFunctionsLoc(t *testing.T) {
	src := "\n// leading comment\nfunc add(a int, b int) int {\n    return a + b\n}\nfunc greet() string {\n    return \"a { b\"\n}\n"
	funcs, lines, err := splitFunctionsLoc(src)
	if err != nil {
		t.Fatalf("splitFunctionsLoc: unexpected error: %v", err)
	}
	if len(funcs) != 2 || len(lines) != 2 {
		t.Fatalf("splitFunctionsLoc: got %d funcs / %d lines, want 2/2: %+v %+v", len(funcs), len(lines), funcs, lines)
	}
	if lines[0] != 3 {
		t.Errorf("splitFunctionsLoc: first func start line = %d, want 3 (blank/comment lines skipped)", lines[0])
	}
	if lines[1] != 6 {
		t.Errorf("splitFunctionsLoc: second func start line = %d, want 6", lines[1])
	}

	if _, _, err := splitFunctionsLoc("func add(a int, b int) int {\n    return a + b\n"); err == nil {
		t.Error("splitFunctionsLoc on unbalanced braces: expected an error, got nil")
	}
}

// TestParseOneDecl covers routing by leading keyword (type/extern/var/func) and
// that a parse error on any branch is propagated.
func TestParseOneDecl(t *testing.T) {
	structs := map[string]bool{}
	if err := parseOneDecl(`type Point struct { x int y int }`, structs); err != nil {
		t.Errorf("parseOneDecl(type): unexpected error: %v", err)
	}
	if err := parseOneDecl(`var count = 0`, structs); err != nil {
		t.Errorf("parseOneDecl(var): unexpected error: %v", err)
	}
	if err := parseOneDecl(`func add(a, b) { return a + b }`, structs); err != nil {
		t.Errorf("parseOneDecl(func): unexpected error: %v", err)
	}
	if err := parseOneDecl(`func add(a, b) { return a + `, structs); err == nil {
		t.Error("parseOneDecl on malformed func: expected an error, got nil")
	}
}
