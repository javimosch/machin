package main

import "testing"

// TestRegexValidPatternNoMatch covers the valid-pattern-but-no-match path for
// the regex builtins (codegen.go mfl_regex_match/_find/_groups/_replace),
// distinct from the malformed-pattern path already covered by
// TestRegexBadPattern in mfl_test.go: regcomp succeeds here, but regexec finds
// nothing, so each builtin must fall back the same way it does on a bad
// pattern (match->false, find->"", groups->empty slice, replace->unchanged).
func TestRegexValidPatternNoMatch(t *testing.T) {
	main := `func main() {
	ok := "false"  if regex_match("abc", "[0-9]+") { ok = "true" }
	g := regex_groups("abc", "[0-9]+")
	println(ok + "|" + regex_find("abc", "[0-9]+") + "|" + str(len(g)) + "|" + regex_replace("abc", "[0-9]+", "x"))
}`
	out, _ := buildRun(t, main)
	if out != "false||0|abc\n" {
		t.Fatalf("regex no-match: got %q", out)
	}
}

// TestRegexGroupsOptionalUnmatched covers mfl_regex_groups' documented
// behavior that an unmatched optional capture group reports "" rather than
// being omitted from the slice.
func TestRegexGroupsOptionalUnmatched(t *testing.T) {
	main := `func main() {
	g := regex_groups("abc", "(abc)(x)?")
	println(str(len(g)) + "|" + g[0] + "|" + g[1] + "|" + g[2])
}`
	out, _ := buildRun(t, main)
	if out != "3|abc|abc|\n" {
		t.Fatalf("regex optional group: got %q", out)
	}
}
