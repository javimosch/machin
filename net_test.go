package main

import (
	"strings"
	"testing"
)

// Issue #4: the networking builtins and the concurrent server example are
// documented, flagship features with no automated coverage. These tests
// type-check the builtins and smoke-compile the examples to C so a regression
// in either path is caught at build time.

// TestNetworkingBuiltinsTypeCheck exercises listen/accept/read/write/close so
// their type signatures stay wired up correctly.
func TestNetworkingBuiltinsTypeCheck(t *testing.T) {
	fns := parseFuncs(t, `func main() {
		fd := listen(8080)
		conn := accept(fd)
		req := read(conn)
		write(conn, "ok: " + req)
		close(conn)
		close(fd)
	}`)
	c, err := CompileToC(fns)
	if err != nil {
		t.Fatalf("networking builtins should compile to C: %v", err)
	}
	for _, sym := range []string{"mfl_listen", "mfl_accept", "mfl_read", "mfl_write", "mfl_close"} {
		if !strings.Contains(c, sym) {
			t.Fatalf("generated C missing %s", sym)
		}
	}
}

// TestExamplesSmokeCompile makes sure the goroutine and concurrent-HTTP-server
// examples still parse, type-check, and generate C.
func TestExamplesSmokeCompile(t *testing.T) {
	for _, path := range []string{
		"examples/complex/goroutines.mfl",
		"examples/complex/http_server.mfl",
	} {
		fns, err := loadMFL(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		c, err := CompileToC(fns)
		if err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		if strings.TrimSpace(c) == "" {
			t.Fatalf("compile %s produced empty C", path)
		}
	}
}

// TestGoroutineRuns confirms goroutines actually execute end-to-end (compile,
// link, run) rather than only compiling.
func TestGoroutineRuns(t *testing.T) {
	out := runNative(t,
		`func work(n) { println("done", n) }`,
		`func main() { go work(1) sleep(50) println("main") }`)
	if !strings.Contains(out, "done 1") || !strings.Contains(out, "main") {
		t.Fatalf("goroutine output unexpected: %q", out)
	}
}
