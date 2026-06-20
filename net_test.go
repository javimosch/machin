package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// buildMFL compiles MFL function sources to a native binary at a temp path,
// round-tripping through base64 like the real machine-first pipeline. It
// returns the binary path; cleanup is registered on the test.
func buildMFL(t *testing.T, funcs ...string) string {
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
	bin, err := os.CreateTemp("", "mfl-net-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	if err := BuildBinary(fns, bin.Name()); err != nil {
		os.Remove(bin.Name())
		t.Fatalf("build: %v", err)
	}
	t.Cleanup(func() { os.Remove(bin.Name()) })
	return bin.Name()
}

// freePort asks the OS for an unused TCP port and releases it so the MFL
// server can bind it. The brief reuse window is acceptable for a local test
// and avoids clashing with a fixed port on busy CI machines.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// dialWithRetry connects to addr, retrying until the MFL server has bound its
// listening socket (it is a separate process we just started).
func dialWithRetry(t *testing.T, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			return c
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never came up at %s", addr)
	return nil
}

// TestHTTPServerEndToEnd exercises the flagship networking builtins
// (listen/accept/read/write/close) and the `go handle(conn)` accept loop:
// it compiles a tiny HTTP server, runs it as a real process, and drives it
// over TCP from the test, asserting the response across several requests so
// the loop's repeated accept + goroutine dispatch is covered.
func TestHTTPServerEndToEnd(t *testing.T) {
	port := freePort(t)
	bin := buildMFL(t,
		`func response() { return "HTTP/1.1 200 OK\r\nContent-Length: 11\r\nConnection: close\r\n\r\nhello world" }`,
		`func handle(conn) { read(conn) write(conn, response()) close(conn) }`,
		fmt.Sprintf(`func main() { server := listen(%d) for { conn := accept(server) go handle(conn) } }`, port),
	)

	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 3; i++ {
		conn := dialWithRetry(t, addr)
		if _, err := fmt.Fprint(conn, "GET / HTTP/1.0\r\n\r\n"); err != nil {
			conn.Close()
			t.Fatalf("request %d: %v", i, err)
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		data, err := io.ReadAll(conn)
		conn.Close()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		got := string(data)
		if !strings.Contains(got, "200 OK") || !strings.Contains(got, "hello world") {
			t.Fatalf("request %d: unexpected response %q", i, got)
		}
	}
}

// TestConcurrentWorkers expands goroutine coverage beyond a single worker:
// three goroutines sleep for increasing durations so their output order is
// deterministic, proving `go f(args)` dispatches independent workers that run
// concurrently with main before the final sleep joins them.
func TestConcurrentWorkers(t *testing.T) {
	got := runNative(t,
		`func work(id, ms) { sleep(ms) println(id) }`,
		`func main() { go work(1, 20) go work(2, 50) go work(3, 80) sleep(140) println("done") }`)
	if got != "1\n2\n3\ndone\n" {
		t.Fatalf("got %q", got)
	}
}
