package main

import (
	"strings"
	"testing"
)

// TestHTTPSPostRuntimeGating mirrors TestHTTPSRuntimeGating (mfl_test.go) for
// https_post: a program calling it must emit both the OpenSSL TLS runtime and
// the mfl_https_post entry point, and must not leak WSS framing it never used.
func TestHTTPSPostRuntimeGating(t *testing.T) {
	prog := &Program{Funcs: parseFuncs(t, `func main() { println(https_post("https://example.com", "body")) }`)}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_https_post") || !strings.Contains(c, "openssl/ssl.h") {
		t.Fatal("a program using https_post must emit mfl_https_post atop the OpenSSL TLS runtime")
	}
	if strings.Contains(c, "mfl_wss_") {
		t.Fatal("an https_post-only program must not emit the WebSocket runtime")
	}
}

// TestTLSBytesRuntimeGating covers tls_read_bytes/tls_write_bytes: both must
// compile down to their *_h helper entry points atop the shared TLS core, same
// as the string-flavored tls_read/tls_write already exercised elsewhere.
func TestTLSBytesRuntimeGating(t *testing.T) {
	prog := &Program{Funcs: parseFuncs(t, `func main() {
		fd := tls_client_fd(0, "example.com")
		tls_write_bytes(fd, from_hex("00ff"))
		b := tls_read_bytes(fd)
		println(to_hex(b))
	}`)}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_tls_read_bytes_h(") {
		t.Fatal("tls_read_bytes must emit mfl_tls_read_bytes_h")
	}
	if !strings.Contains(c, "mfl_tls_write_bytes_h(") {
		t.Fatal("tls_write_bytes must emit mfl_tls_write_bytes_h")
	}
}

// TestWSSSendCloseRuntimeGating covers wss_send/wss_close: a program that
// opens a socket, sends a text frame, and closes it must emit all three
// mfl_wss_* entry points atop the WSS runtime (already gated by
// TestWSSRuntimeGating for wss_open/wss_recv).
func TestWSSSendCloseRuntimeGating(t *testing.T) {
	prog := &Program{Funcs: parseFuncs(t, `func main() {
		c := wss_open("wss://x")
		wss_send(c, "hi")
		wss_close(c)
	}`)}
	out, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mfl_wss_send(") {
		t.Fatal("wss_send must emit mfl_wss_send")
	}
	if !strings.Contains(out, "mfl_wss_close(") {
		t.Fatal("wss_close must emit mfl_wss_close")
	}
	if !strings.Contains(out, "mfl_wss_open") || !strings.Contains(out, "openssl/ssl.h") {
		t.Fatal("a program using wss_* must still emit the WebSocket runtime atop the TLS core")
	}
}
