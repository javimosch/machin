package main

import (
	"strings"
	"testing"
)

// sha1_bytes had no test coverage at all — check it against the well-known
// SHA-1("abc") test vector so a broken digest implementation can't slip
// through silently.
func TestSha1Bytes(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    digest := sha1_bytes(bytes("abc"))
    println("len=" + str(len(digest)))
    println("hex=" + to_hex(digest))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"len=20", "hex=a9993e364706816aba3e25717850c26c9cd0d89",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
