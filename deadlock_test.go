package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// buildRunExit compiles a program, runs it under a wall-clock `timeout` guard (so a
// missed deadlock can't hang the suite), and returns its combined output + exit code.
func buildRunExit(t *testing.T, timeoutSecs int, srcs ...string) (string, int) {
	t.Helper()
	bin, err := os.CreateTemp("", "mfl-deadlock-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, srcs...)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command("timeout", strconv.Itoa(timeoutSecs), bin.Name())
	out, _ := cmd.CombinedOutput()
	return string(out), cmd.ProcessState.ExitCode()
}

// TestDeadlockDetected: a program where every live goroutine ends up parked in a
// receive with no one left to send must be reported as a deadlock (exit 2), not hang.
func TestDeadlockDetected(t *testing.T) {
	cases := []struct {
		name string
		srcs []string
	}{
		{"main-recv-nobody-sends", []string{
			`func main() { ch := make(chan int) v := <-ch println(str(v)) }`}},
		{"await-goroutine-that-never-sends", []string{
			`func worker(done) { }`,
			`func main() { done := make(chan int) go worker(done) v := <-done println(str(v)) }`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := buildRunExit(t, 15, c.srcs...)
			if code != 2 || !strings.Contains(out, "deadlock") {
				t.Fatalf("expected deadlock (exit 2 + message); got code=%d out=%q", code, out)
			}
		})
	}
}

// TestDeadlockNoFalsePositive: a correct producer/consumer must complete normally —
// the detector must never flag it.
func TestDeadlockNoFalsePositive(t *testing.T) {
	out, code := buildRunExit(t, 15,
		`func prod(ch) { i := 0 for i < 5 { ch <- i i = i + 1 } close(ch) }`,
		`func main() { ch := make(chan int) go prod(ch) sum := 0 for v := range ch { sum = sum + v } println("sum:" + str(sum)) }`)
	if code != 0 || !strings.Contains(out, "sum:10") {
		t.Fatalf("producer/consumer should complete cleanly; got code=%d out=%q", code, out)
	}
}
