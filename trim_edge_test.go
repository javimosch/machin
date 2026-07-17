package main

import (
	"strings"
	"testing"
)

// mfl_trim in codegen.go strips leading and trailing whitespace using C's
// isspace() (so tabs, newlines and carriage returns count, not just spaces)
// and preserves interior whitespace. The only existing exercise is a single
// trim("  hi  ") in an omnibus assert (TestStringSplitJoinReplaceTrim), which
// leaves the edges unpinned: an empty/all-whitespace input, non-space
// whitespace classes, a no-whitespace passthrough, interior preservation, and
// one-sided trimming could all silently regress. This locks them in.
func TestTrimEdges(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("empty=[" + trim("") + "]")
    println("all_ws=[" + trim("   ") + "]")
    println("both=[" + trim("  hi  ") + "]")
    println("tabs_nl=[" + trim("\t\nhi world\r\n") + "]")
    println("none=[" + trim("no") + "]")
    println("inner=[" + trim(" a b ") + "]")
    println("lead=[" + trim("\t\r\n  left") + "]")
    println("trail=[" + trim("right \t\r\n") + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"empty=[]",           // nothing to trim -> empty stays empty
		"all_ws=[]",          // all-whitespace collapses to empty
		"both=[hi]",          // spaces stripped from both ends
		"tabs_nl=[hi world]", // tab/newline/CR are whitespace; interior space kept
		"none=[no]",          // no surrounding whitespace -> unchanged
		"inner=[a b]",        // interior whitespace preserved
		"lead=[left]",        // leading-only whitespace stripped
		"trail=[right]",      // trailing-only whitespace stripped
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
