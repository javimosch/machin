package main

import (
	"strings"
	"testing"
)

// Regression coverage for #451: a string literal that contains BOTH brace
// characters ("{"/"}") AND escaped double-quotes ('\"') must not be miscounted
// as unbalanced braces when the loose-source splitter walks a line. The two
// splitters (splitFunctions, used by `machin encode`, and splitFunctionsLoc,
// used by `machin check`) are string- and comment-aware: braces that appear
// inside a string literal are ignored, and a backslash escapes the following
// byte so that '\"' does NOT close the string. These tests pin that contract
// so a future edit to the brace walker cannot silently reintroduce the bug.

// the exact three-function program from the #451 report.
const issue451Src = `func a() (h) { h = "x {id} y" }
func b() (h) { h = "x {\"id\":1} y" }
func main() { println(a() + b()) }`

func TestSplitFunctionsBraceInEscapedString(t *testing.T) {
	blocks, err := splitFunctions(issue451Src)
	if err != nil {
		t.Fatalf("splitFunctions returned error on braces+escaped-quotes: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 function blocks, got %d: %#v", len(blocks), blocks)
	}
	// The escaped-quote body must survive intact in its own block, not bleed
	// into a neighbour because the string was thought to still be open.
	if !strings.Contains(blocks[1], `"x {\"id\":1} y"`) {
		t.Errorf("block[1] lost its escaped-quote string body: %q", blocks[1])
	}
	if !strings.HasPrefix(strings.TrimSpace(blocks[2]), "func main()") {
		t.Errorf("block[2] should be func main, got %q", blocks[2])
	}
}

func TestSplitFunctionsLocBraceInEscapedString(t *testing.T) {
	blocks, lines, err := splitFunctionsLoc(issue451Src)
	if err != nil {
		t.Fatalf("splitFunctionsLoc returned error on braces+escaped-quotes: %v", err)
	}
	if len(blocks) != 3 || len(lines) != 3 {
		t.Fatalf("expected 3 blocks/lines, got %d/%d", len(blocks), len(lines))
	}
	// Each function starts on its own consecutive source line; a miscount would
	// glue blocks together and skew these.
	want := []int{1, 2, 3}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("block %d: start line = %d, want %d", i, lines[i], w)
		}
	}
}

// A grab-bag of adversarial single-function bodies mixing braces, escaped
// quotes, and escaped backslashes. Every one is brace-balanced at the block
// level and must split into exactly one function.
func TestSplitFunctionsBraceStringAdversarial(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"json-object", `func f() (h) { h = "{\"a\":1}" }`},
		{"leading-brace-run", `func f() (h) { h = "}}}{{{ \" }}}" }`},
		{"escaped-backslash-then-brace", `func f() (h) { h = "a\\b{c}" }`},
		{"lone-escaped-quote", `func f() (h) { h = "\"" }`},
		{"concat-brace-quote", `func f() (h) { h = "{" + "\"" + "}" }`},
		{"nested-block-with-json", `func f() (h) { if h == "" { h = "{x}" } }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks, err := splitFunctions(tc.body)
			if err != nil {
				t.Fatalf("splitFunctions(%q) errored: %v", tc.body, err)
			}
			if len(blocks) != 1 {
				t.Fatalf("splitFunctions(%q) = %d blocks, want 1: %#v", tc.body, len(blocks), blocks)
			}
		})
	}
}
