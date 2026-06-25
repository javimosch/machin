package main

import (
	"strings"
	"testing"
)

// system runs a shell command and returns its exit code — for process orchestration
// (e.g. a CLI spawning a detached daemon). stdout of the command goes to the child's
// stdout; the return value is the exit status.
func TestSystemBuiltin(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("ok=" + str(system("true")))        // 0
    println("err=" + str(system("exit 7")))      // 7
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Join(strings.Fields(out), " "); got != "ok=0 err=7" {
		t.Fatalf("system() exit codes = %q, want %q", got, "ok=0 err=7")
	}
}
