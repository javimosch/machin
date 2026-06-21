package main

import (
	"encoding/base64"
	"net"
	"os"
	"os/exec"
	"strings"
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
	if err := BuildBinary(&Program{Funcs: fns}, bin.Name()); err != nil {
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
	if err := BuildBinary(&Program{Funcs: fns}, bin.Name()); err != nil {
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
