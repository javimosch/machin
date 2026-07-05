package main

import (
	"strings"
	"testing"
)

// mfl_substr/mfl_charat in codegen.go clamp negative and out-of-range indices
// rather than reading out of bounds, but that safety net had no test locking
// it in place — a future edit to the clamping logic could silently reintroduce
// an out-of-bounds read.
func TestSubstrAndCharatBounds(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("neg_start=[" + substr("hello", -3, 2) + "]")
    println("past_end=[" + substr("hello", 2, 100) + "]")
    println("start_gt_end=[" + substr("hello", 4, 1) + "]")
    println("all_negative=[" + substr("hello", -5, -1) + "]")
    println("neg_idx=[" + charat("hello", -1) + "]")
    println("oob_idx=[" + charat("hello", 99) + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"neg_start=[he]",
		"past_end=[llo]",
		"start_gt_end=[]",
		"all_negative=[]",
		"neg_idx=[]",
		"oob_idx=[]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
