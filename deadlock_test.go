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

// TestDeadlockCausalReport: the deadlock report must name each parked goroutine (by its
// stable gid path) and the channel it can never receive from — the wait-cycle — and offer
// a JSON form for agents. Two goroutines waiting on each other's channel form a 2-cycle.
func TestDeadlockCausalReport(t *testing.T) {
	srcs := []string{
		`func a(x, y) { v := <-x y <- v }`,
		`func main() { p := make(chan int) q := make(chan int) go a(p, q) r := <-q p <- r println("done") }`,
	}
	// text report: names both goroutines and mentions "channel".
	out, code := buildRunExit(t, 15, srcs...)
	if code != 2 {
		t.Fatalf("expected deadlock exit 2, got %d: %q", code, out)
	}
	for _, want := range []string{"goroutine 0", "goroutine 0.1", "channel #"} {
		if !strings.Contains(out, want) {
			t.Errorf("text report missing %q; got:\n%s", want, out)
		}
	}
	// JSON report (MFL_RR_JSON): structured wait-cycle.
	bin, err := os.CreateTemp("", "mfl-dlj-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, srcs...)}, bin.Name(), false); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("timeout", "15", bin.Name())
	cmd.Env = append(os.Environ(), "MFL_RR_JSON=1")
	jout, _ := cmd.CombinedOutput()
	js := string(jout)
	for _, want := range []string{`"deadlock":true`, `"goroutine":"0"`, `"goroutine":"0.1"`, `"recvOnChannel":`} {
		if !strings.Contains(js, want) {
			t.Errorf("JSON report missing %q; got: %s", want, js)
		}
	}
}

// TestDeadlockSelectSpin: a select whose cases can never fire busy-polls forever; the
// detector must report it as a deadlock (parked in a select), not let it spin. A select
// with a live feeder, or with a default, must not be flagged.
func TestDeadlockSelectSpin(t *testing.T) {
	out, code := buildRunExit(t, 15,
		`func main() { a := make(chan int) b := make(chan int) for { select { case x := <-a: println(str(x)) case y := <-b: println(str(y)) } } }`)
	if code != 2 || !strings.Contains(out, "select") {
		t.Fatalf("expected a select-spin deadlock (exit 2, mentions select); got code=%d out=%q", code, out)
	}
	// a select with a default never spins → completes.
	out, code = buildRunExit(t, 15,
		`func main() { a := make(chan int) n := 0 for n < 3 { select { case x := <-a: println(str(x)) default: n = n + 1 } } println("done") }`)
	if code != 0 || !strings.Contains(out, "done") {
		t.Fatalf("select-with-default should complete; got code=%d out=%q", code, out)
	}
}
