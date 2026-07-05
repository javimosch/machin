package main

import (
	"strings"
	"testing"
)

// sin/pi and charat/bytes_index had no test coverage at all — each is a
// small pure builtin, but a broken codegen case (wrong arg order, wrong
// index base) would otherwise slip through silently.
func TestMathAndStringBuiltins(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("sin0=" + str(sin(0.0)))
    half := pi() / 2.0
    println("sinhalfpi=" + str(sin(half)))
    println("c0=" + charat("hello", 0))
    println("c4=" + charat("hello", 4))
    h := bytes("hello world")
    needle := bytes("world")
    println("idx=" + str(bytes_index(h, needle, 0)))
    println("missing=" + str(bytes_index(h, bytes("xyz"), 0)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"sin0=0", "sinhalfpi=1", "c0=h", "c4=o", "idx=6", "missing=-1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestCharatEdgeCases covers charat boundary conditions: empty string, negative index, and out-of-bounds access.
func TestCharatEdgeCases(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("empty=[" + charat("", 0) + "]")
    println("negative=[" + charat("hello", -1) + "]")
    println("outofbounds=[" + charat("hello", 10) + "]")
    println("single=[" + charat("x", 0) + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"empty=[]",
		"negative=[]",
		"outofbounds=[]",
		"single=[x]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
