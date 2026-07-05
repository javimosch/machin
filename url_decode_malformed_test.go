package main

import (
	"strings"
	"testing"
)

// mfl_url_decode in codegen.go degrades a truncated or invalid %-escape to a
// literal '%' rather than reading past the end of the string or decoding
// garbage, but that safety net only had round-trip coverage (encode then
// decode) — no test ever fed it malformed input directly.
func TestURLDecodeMalformedInput(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("trunc1=[" + url_decode("abc%") + "]")
    println("trunc2=[" + url_decode("abc%4") + "]")
    println("badhex=[" + url_decode("abc%zz") + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"trunc1=[abc%]",
		"trunc2=[abc%4]",
		"badhex=[abc%zz]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
