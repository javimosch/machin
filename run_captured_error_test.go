package main

import (
	"strings"
	"testing"
)

// RunCaptured must surface a BuildBinary failure (here, a call to an
// undefined function, caught during type-checking) rather than attempt to
// run a binary that was never produced.
func TestRunCapturedBuildError(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { undefinedFunc() }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, rerr := RunCaptured(prog)
	if rerr == nil {
		t.Fatal("RunCaptured: expected an error for a call to an undefined function, got nil")
	}
	if out != "" {
		t.Fatalf("RunCaptured: expected empty output on build failure, got %q", out)
	}
	if !strings.Contains(rerr.Error(), "undefined function") {
		t.Fatalf("RunCaptured error = %q, want it to mention the undefined function", rerr.Error())
	}
}
