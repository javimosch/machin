package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// parseFuncs turns readable function sources into *FuncDecl, matching the
// machine-first path used elsewhere in the test suite.
func parseFuncs(t *testing.T, funcs ...string) []*FuncDecl {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		fn, err := ParseFunc(normalize(f))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	return fns
}

// requireCC skips the test when no C compiler is available, matching the
// behaviour of the cc-dependent tests in mfl_test.go.
func requireCC(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(ccPath()); err != nil {
		t.Skipf("no C compiler (%s) available: %v", ccPath(), err)
	}
}

// freePort asks the kernel for an unused TCP port, then releases it so the
// compiled MFL server can bind it. Avoids hard-coding 48080 (which would clash
// with the example server or with parallel test runs).
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestNetworkingCodegen is a fast, socket-free unit test: it confirms that a
// program using every networking builtin type-checks and lowers to C that calls
// the runtime socket shims (mfl_listen/mfl_accept/read/write/mfl_close).
func TestNetworkingCodegen(t *testing.T) {
	fns := parseFuncs(t,
		`func handle(conn) { read(conn) write(conn, "ok") close(conn) }`,
		`func main() { s := listen(48080) c := accept(s) go handle(c) }`,
	)
	if _, err := Check(fns); err != nil {
		t.Fatalf("check: %v", err)
	}
	c, err := CompileToC(fns)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, sym := range []string{"mfl_listen", "mfl_accept", "mfl_read", "mfl_write", "mfl_close", "mfl_go"} {
		if !strings.Contains(c, sym) {
			t.Errorf("emitted C is missing %q\n%s", sym, c)
		}
	}
}

// serverSrc builds a minimal concurrent HTTP server bound to the given port.
// It mirrors examples/complex/http_server.mfl: one goroutine per connection.
//
// NOTE on a current language gap: MFL exposes server-side sockets only
// (listen/accept/read/write/close) and has no client-side dial, so the e2e
// tests below drive the server from Go's net package rather than from MFL.
func serverSrc(port int) []string {
	return []string{
		`func resp() { return "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 5\r\nConnection: close\r\n\r\nMFLOK" }`,
		`func handle(conn) { read(conn) write(conn, resp()) close(conn) }`,
		fmt.Sprintf(`func main() { s := listen(%d) for { c := accept(s) go handle(c) } }`, port),
	}
}

// startServer compiles the MFL server to a native binary and launches it as a
// subprocess, returning the listen address and a cleanup func that always kills
// the process. It polls until the port accepts connections so callers don't race
// the server's startup.
func startServer(t *testing.T, port int) (addr string, cleanup func()) {
	t.Helper()
	fns := parseFuncs(t, serverSrc(port)...)

	dir := t.TempDir()
	bin := filepath.Join(dir, "mflserver")
	if err := BuildBinary(fns, bin); err != nil {
		t.Fatalf("build server: %v", err)
	}

	cmd := exec.Command(bin)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	cleanup = func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}

	addr = fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return addr, cleanup
		}
		time.Sleep(100 * time.Millisecond)
	}
	cleanup()
	t.Fatalf("server never became reachable on %s", addr)
	return "", cleanup
}

// httpGet performs one bare HTTP/1.1 request over a fresh connection, guarded by
// deadlines so a wedged server can never hang the suite.
func httpGet(addr string) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: test\r\n\r\n")); err != nil {
		return "", err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	return string(buf[:n]), nil
}

// TestNetworkingServerE2E builds the example-style server, drives a real HTTP
// request through it, and asserts the response — end-to-end coverage of
// listen/accept/read/write/close on native code.
func TestNetworkingServerE2E(t *testing.T) {
	requireCC(t)
	port := freePort(t)
	addr, cleanup := startServer(t, port)
	defer cleanup()

	resp, err := httpGet(addr)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !strings.HasPrefix(resp, "HTTP/1.1 200 OK") {
		t.Fatalf("unexpected status line: %q", resp)
	}
	if !strings.Contains(resp, "MFLOK") {
		t.Fatalf("missing body in response: %q", resp)
	}
}

// TestNetworkingServerConcurrent fires several simultaneous requests to confirm
// the `go handle(conn)` goroutine-per-connection model serves them all.
func TestNetworkingServerConcurrent(t *testing.T) {
	requireCC(t)
	port := freePort(t)
	addr, cleanup := startServer(t, port)
	defer cleanup()

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := httpGet(addr)
			if err != nil {
				errs[i] = err
				return
			}
			if !strings.Contains(resp, "MFLOK") {
				errs[i] = fmt.Errorf("bad response %d: %q", i, resp)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("request %d: %v", i, err)
		}
	}
}
