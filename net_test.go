package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// buildServer compiles the given readable function sources to a native binary
// at a temp path (the real machine-first path: source -> base64 -> parse ->
// C -> cc). It returns the binary path. The test is skipped if cc is missing.
func buildServer(t *testing.T, funcs ...string) string {
	t.Helper()
	if _, err := exec.LookPath(ccPath()); err != nil {
		t.Skipf("no C compiler (%s) available: %v", ccPath(), err)
	}
	var fns []*FuncDecl
	for _, f := range funcs {
		enc := base64.StdEncoding.EncodeToString([]byte(normalize(f)))
		raw, _ := base64.StdEncoding.DecodeString(enc)
		fn, err := ParseFunc(string(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	bin, err := os.CreateTemp("", "mfl-srv-*")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	bin.Close()
	t.Cleanup(func() { os.Remove(bin.Name()) })
	if err := BuildBinary(fns, bin.Name()); err != nil {
		t.Fatalf("build: %v", err)
	}
	return bin.Name()
}

// freePort asks the OS for an unused TCP port, then releases it so the MFL
// server (which binds with SO_REUSEADDR) can claim it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// dialHTTP connects with a bounded retry loop (the server needs a moment to
// bind after exec) and returns the full response text for "GET / ".
func dialHTTP(t *testing.T, port int) (string, error) {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	var conn net.Conn
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
		return "", err
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// serverFuncs returns a minimal listen/accept/read/write/close + go HTTP
// server bound to the given port. It mirrors examples/complex/http_server.mfl.
func serverFuncs(port int) []string {
	return []string{
		`func page() { return "hi from mfl" }`,
		`func response() { body := page() return "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: " + str(len(body)) + "\r\nConnection: close\r\n\r\n" + body }`,
		`func handle(conn) { read(conn) write(conn, response()) close(conn) }`,
		fmt.Sprintf(`func main() { server := listen(%d) for { conn := accept(server) go handle(conn) } }`, port),
	}
}

// TestHTTPServerLoopback exercises the networking builtins end-to-end:
// listen/accept/read/write/close driven by a `go`-spawned handler, hit by a
// real net.Dial client. Covers the previously untested flagship feature.
func TestHTTPServerLoopback(t *testing.T) {
	port := freePort(t)
	bin := buildServer(t, serverFuncs(port)...)

	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	resp, err := dialHTTP(t, port)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if !strings.HasPrefix(resp, "HTTP/1.1 200 OK") {
		t.Fatalf("bad status line in response: %q", resp)
	}
	if !strings.Contains(resp, "Content-Length: 11") {
		t.Fatalf("missing/wrong Content-Length: %q", resp)
	}
	if !strings.HasSuffix(resp, "hi from mfl") {
		t.Fatalf("missing body: %q", resp)
	}
}

// TestHTTPServerConcurrent fires N concurrent clients at the server to exercise
// the pthread handlers spawned by `go handle(conn)`. All must get a correct
// response with no hang.
func TestHTTPServerConcurrent(t *testing.T) {
	port := freePort(t)
	bin := buildServer(t, serverFuncs(port)...)

	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	// Warm up: ensure the listener is bound before the burst.
	if _, err := dialHTTP(t, port); err != nil {
		t.Fatalf("warmup dial: %v", err)
	}

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := dialHTTP(t, port)
			if err != nil {
				errs[idx] = err
				return
			}
			if !strings.HasPrefix(resp, "HTTP/1.1 200 OK") || !strings.HasSuffix(resp, "hi from mfl") {
				errs[idx] = fmt.Errorf("client %d bad response: %q", idx, resp)
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("concurrent client failed: %v", err)
		}
	}
}
