package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The deadlock detector leaves through _exit, which discards stdio buffers: with
// stdout on a pipe, everything printed before the deadlock vanished, and so did
// the tail of a --record trace.
func TestDeadlockKeepsBufferedOutput(t *testing.T) {
	prog, err := wallsCompose(t, `func main() {
    println("before one")
    println("before two")
    c := make(chan int)
    println(<-c)
}`)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "p")
	if err := BuildBinary(prog, bin, false); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "trace")
	for _, env := range [][]string{nil, {"MFL_RR_RECORD=" + trace}} {
		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(), env...)
		var o, e strings.Builder
		cmd.Stdout, cmd.Stderr = &o, &e
		_ = cmd.Run()
		if code := cmd.ProcessState.ExitCode(); code != 2 || o.String() != "before one\nbefore two\n" || !strings.Contains(e.String(), "deadlock") {
			t.Errorf("env=%v: code=%d stdout=%q stderr=%q", env, code, o.String(), e.String())
		}
	}
	// one "S <goroutine>" line per print that happened
	b, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\nS "); n < 2 {
		t.Errorf("trace lost its schedule (%d S lines):\n%s", n, b)
	}
}
