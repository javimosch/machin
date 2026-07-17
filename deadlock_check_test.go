package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dlCheck parses + typechecks a program and returns the compile-time deadlock findings.
func dlCheck(t *testing.T, decls ...string) []dlFinding {
	t.Helper()
	nd := make([]string, len(decls))
	for i, d := range decls {
		nd[i] = normalize(d)
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return detectDeadlocks(prog, c)
}

func hasDL(fs []dlFinding, ch string) bool {
	for _, f := range fs {
		if f.Code == "DL001" && f.Chan == ch {
			return true
		}
	}
	return false
}

// TestDeadlockFinderFlags: a receive on a channel nothing ever sends to or closes is a
// guaranteed deadlock, reported at compile time.
func TestDeadlockFinderFlags(t *testing.T) {
	cases := []struct {
		name string
		ch   string
		srcs []string
	}{
		{"main-recv-nobody-sends", "ch", []string{
			`func main() { ch := make(chan int) v := <-ch println(str(v)) }`}},
		{"goroutine-that-never-sends", "ch", []string{
			`func worker(c) { }`,
			`func main() { ch := make(chan int) go worker(ch) v := <-ch println(str(v)) }`}},
		{"range-over-never-fed", "ch", []string{
			`func main() { ch := make(chan int) s := 0 for v := range ch { s = s + v } println(str(s)) }`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !hasDL(dlCheck(t, c.srcs...), c.ch) {
				t.Fatalf("expected DL001 on %q", c.ch)
			}
		})
	}
}

// TestDeadlockFinderClean: fed channels (direct send, close, goroutine feed, indirect feed
// through a call chain) must NOT be flagged — false-positive-free.
func TestDeadlockFinderClean(t *testing.T) {
	cases := []struct {
		name string
		srcs []string
	}{
		{"goroutine-sends", []string{
			`func send(c) { c <- 7 }`,
			`func main() { ch := make(chan int) go send(ch) v := <-ch println(str(v)) }`}},
		{"producer-closes", []string{
			`func prod(c) { i := 0 for i < 3 { c <- i i = i + 1 } close(c) }`,
			`func main() { ch := make(chan int) go prod(ch) s := 0 for v := range ch { s = s + v } println(str(s)) }`}},
		{"indirect-feed-through-call", []string{
			`func inner(c) { c <- 42 }`,
			`func outer(c) { inner(c) }`,
			`func main() { ch := make(chan int) go outer(ch) v := <-ch println(str(v)) }`}},
		{"same-function-send-and-recv", []string{
			`func main() { ch := make(chan int) go feed(ch) x := <-ch println(str(x)) }`,
			`func feed(c) { c <- 1 }`}},
		{"escape-passed-to-unknown-is-conservative", []string{
			// ch is passed to a builtin-ish/unknown position; we can't prove it's unfed, so no DL001.
			`func handler(c) { c <- 9 }`,
			`func main() { ch := make(chan int) go handler(ch) v := <-ch println(str(v)) }`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if fs := dlCheck(t, c.srcs...); len(fs) != 0 {
				t.Fatalf("expected no deadlock finding, got %+v", fs)
			}
		})
	}
}

// TestDeadlockFinderScanCoverage exercises the classifier across statement/expression
// shapes — select, comma-ok receive, arena/while/if bodies, and channels appearing in
// nested expression positions (which conservatively count as a feed / escape). None of
// these should false-positive.
func TestDeadlockFinderScanCoverage(t *testing.T) {
	clean := [][]string{
		// select with a receive case + a send case (both channels fed/used).
		{`func feed(c) { c <- 1 }`,
			`func main() { a := make(chan int) b := make(chan int) go feed(a) go feed(b) select { case v := <-a: println(str(v)) case b <- 9: println("sent") } }`},
		// comma-ok receive inside a while, under an arena, guarded by an if.
		{`func feed(c) { c <- 1 close(c) }`,
			`func main() { ch := make(chan int) go feed(ch) n := 0 while n < 1 { arena { v, ok := <-ch if ok { println(str(v)) } } n = n + 1 } }`},
		// nested expressions (index/binary/struct/slice/call) are walked for channel uses;
		// the channel here is fed by a goroutine, so no finding.
		{`type Box struct { c int }`,
			`func take(xs) (r) { r = xs[0] }`,
			`func feed(c) { c <- 1 }`,
			`func main() { ch := make(chan int) xs := []int{1, 2, 3} b := Box{take(xs) + xs[0]} go feed(ch) v := <-ch println(str(b.c + v)) }`},
	}
	for i, srcs := range clean {
		if fs := dlCheck(t, srcs...); len(fs) != 0 {
			t.Errorf("case %d: expected no deadlock finding, got %+v", i, fs)
		}
	}

	// a diagnostic renders with the deadlock phase + DL001 code.
	fs := dlCheck(t, `func main() { ch := make(chan int) v := <-ch println(str(v)) }`)
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %d", len(fs))
	}
	d := fs[0].toDiagnostic()
	if d.Phase != "deadlock" || d.Code != "DL001" || d.Severity != "warning" {
		t.Fatalf("diagnostic = phase %q code %q sev %q", d.Phase, d.Code, d.Severity)
	}
}

// TestDeadlockTestCommand exercises the `machin deadlocktest` oracle command (the
// self-hosting oracle): it dumps DL001 findings in the canonical hex form, and handles
// the no-arg / missing-file / parse-error / check-error paths.
func TestDeadlockTestCommand(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// a program with a DL001 finding — capture stdout to confirm a canonical line is dumped.
	dl := write("dl.mfl", "func main() { ch := make(chan int) v := <-ch println(str(v)) }\n")
	out := captureStdoutDL(t, func() {
		if err := cmdDeadlockTest([]string{"--program", dl}); err != nil {
			t.Fatalf("deadlocktest: %v", err)
		}
	})
	if !strings.Contains(out, "|444c303031|") { // hex("DL001")
		t.Fatalf("expected a DL001 hex line, got %q", out)
	}

	// clean program — no findings, empty output.
	clean := write("clean.mfl", "func s(c) { c <- 1 }\nfunc main() { ch := make(chan int) go s(ch) v := <-ch println(str(v)) }\n")
	if out := captureStdoutDL(t, func() { _ = cmdDeadlockTest([]string{"--program", clean}) }); strings.TrimSpace(out) != "" {
		t.Fatalf("clean program should dump nothing, got %q", out)
	}

	// error / edge paths.
	if err := cmdDeadlockTest(nil); err == nil {
		t.Fatal("expected usage error with no --program")
	}
	if err := cmdDeadlockTest([]string{"--program", filepath.Join(dir, "nope.mfl")}); err == nil {
		t.Fatal("expected error for a missing file")
	}
	perr := write("perr.mfl", "func main() { x := }\n")
	if out := captureStdoutDL(t, func() { _ = cmdDeadlockTest([]string{"--program", perr}) }); !strings.Contains(out, "(parse-error)") && !strings.Contains(out, "(check-error)") {
		t.Fatalf("malformed source should report parse/check-error, got %q", out)
	}
}

// captureStdoutDL runs fn with os.Stdout redirected to a pipe and returns what it wrote.
func captureStdoutDL(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = saved
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return b.String()
}
