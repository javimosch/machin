package main

import (
	"strings"
	"testing"
)

// RunCaptured used to return only the child's stdout, so a program that died
// arrived as a bare "signal: aborted (core dumped)" or "exit status 1" with
// every word of explanation thrown away — no glibc abort message, no deadlock
// report, no sanitizer trace, no "bind: Address already in use".
//
// That is what made a websocket test that failed once and never again
// impossible to diagnose: by the time anyone looked, the only evidence had
// already been discarded by the harness.

// TestRunCapturedIncludesStderrOnFailure: a program that writes to stderr and
// exits non-zero reports both facts.
func TestRunCapturedIncludesStderrOnFailure(t *testing.T) {
	// A deadlock is a realistic instance of the problem: the runtime explains
	// itself in detail on stderr and exits 2, and every word of that
	// explanation used to be discarded by the harness.
	prog := progFromSrc(t, `
func main() {
    println("on stdout")
    flush()
    c := make(chan int)
    x := <-c
    println(str(x))
}`)
	out, err := RunCaptured(prog)
	if err == nil {
		t.Fatal("expected a non-zero exit")
	}
	if !strings.Contains(err.Error(), "deadlock") {
		t.Fatalf("stderr was dropped from the error: %v", err)
	}
	if !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("the exit status went missing: %v", err)
	}
	if !strings.Contains(out, "on stdout") {
		t.Fatalf("stdout should still be returned separately, got %q", out)
	}
}

// ...and a program that succeeds is unaffected: no stderr, no decoration.
func TestRunCapturedCleanRunUnchanged(t *testing.T) {
	prog := progFromSrc(t, `func main() { println("fine") }`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "fine" {
		t.Fatalf("unexpected stdout %q", out)
	}
}
