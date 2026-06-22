package main

import (
	"encoding/base64"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// parseFuncs parses readable function sources through the real base64 path,
// mirroring runNative but without compiling — used by tests that only need the
// type checker's verdict.
func parseFuncs(t *testing.T, funcs ...string) []*FuncDecl {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		enc := base64.StdEncoding.EncodeToString([]byte(normalize(f)))
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("b64: %v", err)
		}
		fn, err := ParseFunc(string(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	return fns
}

// checkErr asserts that type-checking the given program fails with a message
// containing want — i.e. the error is a clean MFL diagnostic, not leaked cc
// output from a downstream compile failure.
func checkErr(t *testing.T, want string, funcs ...string) {
	t.Helper()
	_, err := Check(&Program{Funcs: parseFuncs(t, funcs...)})
	if err == nil {
		t.Fatalf("expected type error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %q", want, err.Error())
	}
}

// --- Issue #1: string ==/!= compare by value, not by pointer. ---

func TestStringEqualityByValue(t *testing.T) {
	// b is built at runtime via concatenation, so it is a distinct pointer from
	// the literal a. A pointer compare would wrongly report "notequal".
	got := runNative(t, `func main() { a := "ab" b := "a" + "b" if a == b { println("equal") } else { println("notequal") } }`)
	if got != "equal\n" {
		t.Fatalf("string == by value: got %q, want \"equal\\n\"", got)
	}
}

func TestStringInequalityByValue(t *testing.T) {
	got := runNative(t, `func main() { a := "abc" b := "abd" if a != b { println("differ") } else { println("same") } }`)
	if got != "differ\n" {
		t.Fatalf("string != by value: got %q, want \"differ\\n\"", got)
	}
}

// --- Issue #2: % on floats is a clean MFL type error, not leaked cc output. ---

func TestModuloFloatRejected(t *testing.T) {
	checkErr(t, "mismatch", `func main() { x := 5.0 y := 2.0 println(x % y) }`)
}

func TestModuloIntStillWorks(t *testing.T) {
	if got := runNative(t, `func main() { println(7 % 3) }`); got != "1\n" {
		t.Fatalf("int %% regressed: got %q", got)
	}
}

// --- Issue #3: len() on a non-string/non-slice is a type error, never strlen() on a scalar. ---

func TestLenOnIntRejected(t *testing.T) {
	checkErr(t, "len: argument must be", `func main() { println(len(5)) }`)
}

func TestLenOnStringAndSliceStillWorks(t *testing.T) {
	if got := runNative(t, `func main() { s := "hello" println(len(s)) }`); got != "5\n" {
		t.Fatalf("len(string) regressed: got %q", got)
	}
}

// --- --safe: bounds, div-by-zero, and overflow checks panic at runtime. ---

func TestSafeChecks(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"bounds", `func main() { xs := []int{1, 2, 3} println(xs[5]) }`, "index out of range"},
		{"divzero", `func main() { a := 7 b := 0 println(a / b) }`, "divide by zero"},
		{"overflow", `func main() { x := 9000000000000000000 println(x + x) }`, "overflow"},
	}
	for _, c := range cases {
		bin, err := os.CreateTemp("", "mfl-safe-*")
		if err != nil {
			t.Fatal(err)
		}
		bin.Close()
		defer os.Remove(bin.Name())
		if err := BuildBinary(&Program{Funcs: parseFuncs(t, c.src)}, bin.Name(), true); err != nil {
			t.Fatalf("%s: build: %v", c.name, err)
		}
		out, err := exec.Command(bin.Name()).CombinedOutput()
		if err == nil {
			t.Fatalf("%s: expected a non-zero exit (a panic)", c.name)
		}
		if !strings.Contains(string(out), c.want) {
			t.Fatalf("%s: got %q, want substring %q", c.name, out, c.want)
		}
	}
}

// --- input(): read a line from stdin (interactive CLI / desktop programs). ---

func TestInputBuiltin(t *testing.T) {
	bin, err := os.CreateTemp("", "mfl-input-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	src := `func main() { print("? ") n := input() println("got", n, len(n)) }`
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(bin.Name())
	cmd.Stdin = strings.NewReader("hello\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// the trailing newline is stripped, so len is 5, not 6
	if got, want := string(out), "? got hello 5\n"; got != want {
		t.Fatalf("input(): got %q, want %q", got, want)
	}
}

// --- By-reference closure capture: closures share mutable captured state. ---

// A counter() returns a closure that mutates a captured local. Each call must
// observe the previous mutation (1, 2, 3), and a second counter must have an
// independent cell — proving capture is by reference, not a value snapshot.
func TestClosureCaptureByReference(t *testing.T) {
	got := runNative(t,
		`func counter() { n := 0 return func() { n = n + 1 return n } }`,
		`func main() { c := counter() d := counter() println(c(), c(), c(), d(), c()) }`)
	if got != "1 2 3 1 4\n" {
		t.Fatalf("by-reference capture: got %q, want \"1 2 3 1 4\\n\"", got)
	}
}

// Two closures over the same variable share one cell: a mutation through one is
// visible through the other (and to the enclosing scope).
func TestClosureSharedCell(t *testing.T) {
	got := runNative(t,
		`func build() { n := 100 inc := func() { n = n + 1 } get := func() { return n } inc() inc() return get() }`,
		`func main() { println(build()) }`)
	if got != "102\n" {
		t.Fatalf("shared capture cell: got %q, want \"102\\n\"", got)
	}
}

// --- Scoped arenas: `arena { }` bounds the memory of a long-lived loop. ---

// buildRun compiles a single-function program, runs it, and returns its stdout
// plus the child's peak RSS in KB (Linux getrusage via ProcessState).
func buildRun(t *testing.T, srcs ...string) (string, int64) {
	t.Helper()
	bin, err := os.CreateTemp("", "mfl-arena-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, srcs...)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(bin.Name())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	ru, _ := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	return string(out), ru.Maxrss
}

// A loop that allocates each iteration grows memory unboundedly when run on the
// goroutine arena, but stays flat when the per-iteration work is wrapped in an
// `arena { }` block. The output must be identical either way, and the scoped
// version's peak RSS must be a small fraction of the unscoped version's.
func TestScopedArenaBoundsMemory(t *testing.T) {
	const work = `func work(i) { s := "iter-" + str(i) s = s + "-" + str(i * i) return len(s) }`
	loop := func(scoped bool) string {
		body := `total = total + work(n)`
		if scoped {
			body = `arena { total = total + work(n) }`
		}
		return `func main() { total := 0 n := 0 while n < 400000 { ` + body + ` n = n + 1 } println(total) }`
	}
	unscopedOut, unscopedRSS := buildRun(t, loop(false), work)
	scopedOut, scopedRSS := buildRun(t, loop(true), work)

	if scopedOut != unscopedOut {
		t.Fatalf("arena changed program output: scoped %q vs unscoped %q", scopedOut, unscopedOut)
	}
	// The unscoped loop retains ~all allocations; the scoped one frees each
	// iteration. Require at least a 5x reduction (the real gap is ~100x).
	if scopedRSS*5 >= unscopedRSS {
		t.Fatalf("arena did not bound memory: scoped RSS %d KB vs unscoped %d KB", scopedRSS, unscopedRSS)
	}
}

// --- Issue #4: networking builtins + concurrent server compile and run. ---

// TestNetworkingExampleCompiles builds the documented concurrent HTTP server
// (listen/accept/read/write/close + a `go` goroutine) end-to-end through cc,
// proving the flagship networking example actually codegens and links.
func TestNetworkingExampleCompiles(t *testing.T) {
	fns := parseFuncs(t,
		`func page() { return "<h1>hi</h1>" }`,
		`func response() { body := page() return "HTTP/1.1 200 OK\r\nContent-Length: " + str(len(body)) + "\r\n\r\n" + body }`,
		`func handle(conn) { read(conn) write(conn, response()) close(conn) }`,
		`func main() { server := listen(0) for { conn := accept(server) go handle(conn) } }`)
	bin, err := os.CreateTemp("", "mfl-net-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: fns}, bin.Name(), false); err != nil {
		t.Fatalf("networking example failed to compile: %v", err)
	}
}

// TestNetworkingRoundTrip exercises the socket builtins live: an MFL server
// accepts one connection, reads the request, writes a reply and exits. A Go
// client drives it and verifies the bytes make the full round trip.
func TestNetworkingRoundTrip(t *testing.T) {
	const port = 47654
	fns := parseFuncs(t,
		`func main() { s := listen(`+itoa(port)+`) conn := accept(s) read(conn) write(conn, "pong") close(conn) }`)
	bin, err := os.CreateTemp("", "mfl-srv-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: fns}, bin.Name(), false); err != nil {
		t.Fatalf("server failed to compile: %v", err)
	}

	cmd := exec.Command(bin.Name())
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// The server needs a moment to bind; retry-dial until it is listening.
	var conn net.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+itoa(port), 200*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("could not connect to MFL server: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write to server: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read from server: %v", err)
	}
	if got := string(buf[:n]); got != "pong" {
		t.Fatalf("round trip: got %q, want \"pong\"", got)
	}
}

// TestDialOutbound exercises client networking: a Go server listens, an MFL
// program dials it with dial(host, port), writes a request, reads the reply.
func TestDialOutbound(t *testing.T) {
	const port = 47656
	ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		c.Read(buf)
		c.Write([]byte("pong-from-server"))
		c.Close()
	}()

	fns := parseFuncs(t,
		`func main() { fd := dial("127.0.0.1", `+itoa(port)+`) if fd < 0 { print("dial failed") return } write(fd, "ping") r := read(fd) close(fd) print(r) }`)
	bin, err := os.CreateTemp("", "mfl-dial-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: fns}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := string(out); got != "pong-from-server" {
		t.Fatalf("dial round trip: got %q, want \"pong-from-server\"", got)
	}
}

// itoa is a tiny dependency-free int->string for embedding ports in MFL source.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
