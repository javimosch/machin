package main

import (
	"os"
	"strings"
	"testing"
)

// wsProg composes machweb.src + ws.src + the test app (ws.src needs machweb's hijack).
func wsProg(t *testing.T, app string) *Program {
	t.Helper()
	mw, err := os.ReadFile("framework/machweb.src")
	if err != nil {
		t.Skip("framework/machweb.src not found")
	}
	ws, err := os.ReadFile("framework/ws.src")
	if err != nil {
		t.Skip("framework/ws.src not found")
	}
	prog, perr := progFromSrcErr(string(mw) + string(ws) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// A full WebSocket round-trip over the socket: the server upgrades (RFC 6455 handshake)
// and echoes; the client sends a hand-built *masked* "hi" frame and reads the server's
// *unmasked* echo frame. Proves the handshake accept key, client-frame unmasking, and
// server-frame building all in the real socket path.
//
//	masked "hi": 81 82 | 01020304 (mask) | 69 6b  ('h'^01, 'i'^02)
//	key "dGhlIHNhbXBsZSBub25jZQ==" -> accept "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" (the RFC example)
func TestWebSocketEchoRoundTrip(t *testing.T) {
	app := `
func serve_one(srv, handler) { conn := accept(srv)  machweb_handle(conn, handler) }
func echo(req) (res) {
    res = ws(req, func(c) {
        while 1 == 1 {
            msg, ok := ws_next_text(c)
            if ok == 0 { return }
            s := ws_send_text(c.fd, "echo: " + msg)
            if s < 0 { return }
        }
    })
}
func read_some(c) (s) {
    s = ""
    i := 0
    while i < 6 {
        chunk := read(c)
        if chunk == "" { return }
        s = s + chunk
        i = i + 1
    }
}
func main() {
    port := 48241
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return echo(req) })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    write(c, "GET /ws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
    sleep(40)
    hs := read(c)
    write_bytes(c, from_hex("818201020304696b"))     // a masked "hi" frame
    sleep(40)
    frame := read(c)
    close(c)
    println("HS<" + hs + ">")
    println("FRAME<" + frame + ">")
}`
	out, err := RunCaptured(wsProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "listen-failed") || strings.Contains(out, "dial-failed") {
		t.Fatalf("loopback setup failed:\n%s", out)
	}
	// the upgrade handshake completed with the correct RFC 6455 accept key
	if !strings.Contains(out, "101 Switching Protocols") || !strings.Contains(out, "Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=") {
		t.Fatalf("handshake should be a 101 with the correct accept key; got:\n%s", out)
	}
	// the client's masked "hi" was unmasked and echoed back in an (unmasked) text frame
	if !strings.Contains(out, "echo: hi") {
		t.Fatalf("server should unmask the client frame and echo it; got:\n%s", out)
	}
}
