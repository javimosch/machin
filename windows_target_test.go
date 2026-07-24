package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// #517 Phase 0: the windows target compiles the POSIX-independent core. These
// checks are toolchain-free (they exercise CompileToCTarget / the preflight),
// mirroring the zig-free branches the wasm tests cover.

// The windows preflight must reject each not-yet-supported subsystem with an
// actionable, #517-referencing message — before any C is handed to a compiler.
func TestWindowsPreflightRejects(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		mentions string
	}{
		{"tty", `func main() { raw_mode(1)  println(read_key()) }`, "terminal raw mode"},
		{"sqlite", `func main() { sqlite_open("x.db") }`, "SQLite"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prog, err := ParseProgram([]string{normalize(c.src)})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, _, err = CompileToCTarget(prog, false, targetWindows)
			if err == nil {
				t.Fatalf("expected windows preflight to reject %s, got nil", c.name)
			}
			if !strings.Contains(err.Error(), c.mentions) || !strings.Contains(err.Error(), "#517") {
				t.Fatalf("error should mention %q and #517, got: %v", c.mentions, err)
			}
		})
	}
}

// A stdio/compute/goroutine/json program must pass the windows preflight, and
// the emitted C must NOT pull in the POSIX socket/tty runtime (which native
// always emits) — that's what lets it cross-compile without winsock/termios.
func TestWindowsCoreOmitsPosixRuntime(t *testing.T) {
	typ := `type P struct { name string  age int }`
	main := `func main() {
	xs := []P{}
	xs = append(xs, P{name: "x", age: 3})
	println(json(xs))
	ch := make(chan int)
	go send42(ch)
	println(str(<-ch))
}`
	send := `func send42(ch) { ch <- 42 }`

	prog, err := ParseProgram([]string{normalize(typ), normalize(send), normalize(main)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	winC, _, err := CompileToCTarget(prog, false, targetWindows)
	if err != nil {
		t.Fatalf("windows compile: %v", err)
	}
	// mfl_listen / mfl_raw_mode are net/tty-runtime symbols; native emits them
	// unconditionally, windows must not for a program that uses neither.
	for _, sym := range []string{"mfl_listen(", "mfl_raw_mode("} {
		if strings.Contains(winC, sym) {
			t.Fatalf("windows C should not contain the POSIX-runtime symbol %q", sym)
		}
	}
	// sanity: the same program on native DOES emit that runtime, proving the
	// difference is target-driven, not that the symbol simply moved.
	natC, _, err := CompileToCTarget(prog, false, targetNative)
	if err != nil {
		t.Fatalf("native compile: %v", err)
	}
	if !strings.Contains(natC, "mfl_listen(") {
		t.Fatal("native C unexpectedly lacks the socket runtime — test assumption broken")
	}
}

// #517 Phase N: TCP sockets (dial/listen/accept/read/write/close) are now
// supported on the windows target via winsock2, so a net program must pass the
// preflight and emit the winsock startup + the ws2_32 dependency marker.
func TestWindowsNetCompiles(t *testing.T) {
	// a client (dial) and a server (listen/accept) both exercise the socket runtime
	src := `func main() {
	fd := dial("example.com", 80)
	write(fd, "GET / HTTP/1.0\r\n\r\n")
	println(read(fd))
	close(fd)
	sfd := listen(8080)
	c := accept(sfd)
	println(peer_addr(c))
	close(c)
	close(sfd)
}`
	prog, err := ParseProgram([]string{normalize(src)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	winC, _, err := CompileToCTarget(prog, false, targetWindows)
	if err != nil {
		t.Fatalf("windows net compile should succeed (Phase N), got: %v", err)
	}
	// winsock is initialized (WSAStartup) and the socket runtime is present.
	if !strings.Contains(winC, "WSAStartup") {
		t.Fatal("windows net C should call WSAStartup (winsock init)")
	}
	if !strings.Contains(winC, "closesocket") {
		t.Fatal("windows net C should use closesocket, not close(), on sockets")
	}
	// end-to-end link to a PE (gated on a runnable zig — links ws2_32)
	if err := exec.Command(zigPath(), "version").Run(); err != nil {
		t.Skipf("zig not runnable (%v) — skipping windows net PE link", err)
	}
	out := t.TempDir() + "/net.exe"
	if err := BuildWindows(prog, out, false); err != nil {
		t.Fatalf("BuildWindows (net): %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 2 || b[0] != 'M' || b[1] != 'Z' {
		t.Fatalf("net output is not a PE executable: first bytes %q", b[:min(8, len(b))])
	}
}

// #517 Phase TLS: HTTPS/TLS + the OpenSSL crypto builtins now compile for the
// windows target, linking a user-supplied mingw OpenSSL. The preflight no longer
// rejects them, the emitted C includes OpenSSL + the strcasestr shim, and
// BuildWindows demands MACHIN_WIN_OPENSSL (a clear error, not a cryptic link fail).
func TestWindowsTLSCompiles(t *testing.T) {
	src := `func main() {
	code, body, err := http_get("https://example.com")
	println(str(code) + " " + str(len(body)) + err)
}`
	prog, err := ParseProgram([]string{normalize(src)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// preflight lifted: TLS compiles to C for windows
	winC, _, err := CompileToCTarget(prog, false, targetWindows)
	if err != nil {
		t.Fatalf("windows TLS compile should succeed (Phase TLS), got: %v", err)
	}
	if !strings.Contains(winC, "openssl/ssl.h") {
		t.Fatal("windows TLS C should include OpenSSL")
	}
	if !strings.Contains(winC, "mfl_strcasestr") {
		t.Fatal("windows TLS C should define the mingw strcasestr shim")
	}

	// BuildWindows must demand a user-supplied OpenSSL with a clear message.
	t.Setenv("MACHIN_WIN_OPENSSL", "")
	err = BuildWindows(prog, t.TempDir()+"/a.exe", false)
	if err == nil || !strings.Contains(err.Error(), "MACHIN_WIN_OPENSSL") {
		t.Fatalf("expected a MACHIN_WIN_OPENSSL requirement error, got: %v", err)
	}

	// End-to-end link, gated on BOTH a runnable zig and a mingw OpenSSL being
	// present (CI has neither → skips); proves the winsock+OpenSSL link is real.
	sslDir := os.Getenv("MACHIN_WIN_OPENSSL_TEST")
	if sslDir == "" {
		t.Skip("set MACHIN_WIN_OPENSSL_TEST to a mingw OpenSSL dir to link-test windows TLS")
	}
	if err := exec.Command(zigPath(), "version").Run(); err != nil {
		t.Skipf("zig not runnable: %v", err)
	}
	t.Setenv("MACHIN_WIN_OPENSSL", sslDir)
	out := t.TempDir() + "/https.exe"
	if err := BuildWindows(prog, out, false); err != nil {
		t.Fatalf("BuildWindows (TLS): %v", err)
	}
	b, _ := os.ReadFile(out)
	if len(b) < 2 || b[0] != 'M' || b[1] != 'Z' {
		t.Fatal("TLS output is not a PE executable")
	}
}

// End-to-end: cross-compile a real program to a Windows PE. Gated on a working
// zig (the snap wrapper is often broken; set $ZIG to the real binary), so CI
// without a windows-capable zig skips rather than fails.
func TestWindowsBuildsPE(t *testing.T) {
	if err := exec.Command(zigPath(), "version").Run(); err != nil {
		t.Skipf("zig not runnable (%v) — skipping windows PE build", err)
	}
	prog, err := ParseProgram([]string{normalize(`func main() {
	i := 0
	total := 0
	while i < 100 { arena { s := "row-" + str(i)  total = total + len(s) }  i = i + 1 }
	println("total=" + str(total))
	arena_reset()
}`)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := t.TempDir() + "/a.exe"
	if err := BuildWindows(prog, out, false); err != nil {
		t.Fatalf("BuildWindows: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 2 || b[0] != 'M' || b[1] != 'Z' {
		t.Fatalf("output is not a PE executable (missing MZ header): first bytes %q", b[:min(8, len(b))])
	}
}
