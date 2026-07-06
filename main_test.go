package main

import (
	"os"
	"strings"
	"testing"
)

// usage() had no direct test coverage; verify it prints the command summary to
// stderr rather than stdout, and lists every top-level subcommand a user can run.
func TestUsage(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	usage()
	w.Close()
	os.Stderr = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	for _, want := range []string{
		"machin run", "machin build", "machin encode", "machin check",
		"machin test", "machin pack", "machin guide", "machin skill install",
		"machin framework",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage() output missing %q, got:\n%s", want, out)
		}
	}
}
