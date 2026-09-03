package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// listen() reports a bind failure by returning -1, so the guard every MFL server
// is written with actually runs.
//
// It used to call exit(1), which made `if srv < 0 { ... }` dead code in every
// server ever written against the documented `(int) -> int` signature — the one
// case the guard existed for was the one case it could not catch (#643).
func TestListenReturnsMinusOneOnBindFailure(t *testing.T) {
	// hold the port for real, and keep holding it while the MFL program runs
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Skipf("no loopback available: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	out, err := RunCaptured(progFromSrc(t, fmt.Sprintf(`
func main() {
    srv := listen(%d)
    if srv < 0 { println("guard works")  return }
    println("bound the taken port: " + str(srv))
}`, port)))
	if err != nil {
		t.Fatalf("a taken port must not kill the process: %v", err)
	}
	if !strings.Contains(out, "guard works") {
		t.Fatalf("the `if srv < 0` guard should have run; got:\n%s", out)
	}
}

// The happy path still hands back a usable fd — the fix must not turn every
// successful listen into a failure.
func TestListenStillSucceedsOnAFreePort(t *testing.T) {
	out, err := RunCaptured(progFromSrc(t, fmt.Sprintf(`
func main() {
    srv := listen(%d)
    if srv < 0 { println("unexpectedly failed")  return }
    close(srv)
    println("bound")
}`, freeLoopbackPort(t))))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "bound") {
		t.Fatalf("a free port should bind; got:\n%s", out)
	}
}

// An unguarded caller must not spin. `for { accept(server) }` with server == -1
// is an unrecoverable mistake, not a transient accept error: it says so and
// stops, rather than pegging a core forever.
func TestAcceptOnNegativeFdReportsInsteadOfSpinning(t *testing.T) {
	out, err := RunCaptured(progFromSrc(t, `
func main() {
    conn := accept(0 - 1)
    println("returned " + str(conn))
}`))
	if err == nil {
		t.Fatalf("accept(-1) should not succeed; output:\n%s", out)
	}
	// RunCaptured folds the child's stderr into the error (#642), which is the
	// only place this message can appear.
	if !strings.Contains(err.Error(), "not a listening socket") {
		t.Fatalf("the error should name the cause; got: %v\noutput:\n%s", err, out)
	}
}
