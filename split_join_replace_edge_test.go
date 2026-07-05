package main

import (
	"strings"
	"testing"
)

// mfl_replace/mfl_split in codegen.go each special-case an empty
// separator/pattern (replace: return s unchanged; split: split into
// individual characters) but neither edge case had test coverage — a
// future rewrite of the loop logic could silently regress into an
// infinite loop or an out-of-bounds read.
func TestSplitJoinReplaceEdgeCases(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("replace_empty_pat=[" + replace("abc", "", "X") + "]")
    parts := split("hi", "")
    println("split_empty_sep_len=" + str(len(parts)))
    println("split_empty_sep_joined=[" + join(parts, "-") + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"replace_empty_pat=[abc]",
		"split_empty_sep_len=2",
		"split_empty_sep_joined=[h-i]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
