package main

import (
	"strings"
	"testing"
)

// Integration tests for machweb's socket I/O path — the part the pure-logic tests
// can't reach. Each program is a one-shot loopback: main listens, a goroutine
// accepts + runs machweb_handle (read_request -> parse -> handler -> write), and
// main itself is the HTTP client (dial -> write -> read). This exercises the real
// read/write/close builtins, not just the parsing functions.
//
// read_all drains the socket until the server closes (read returns "").
const loopbackHelpers = `
func serve_one(srv, handler) {
    conn := accept(srv)
    machweb_handle(conn, handler)
}
func read_all(c) (s) {
    s = ""
    while 1 == 1 {
        chunk := read(c)
        if chunk == "" { return }
        s = s + chunk
    }
}`

// read_request assembles a body that arrives in MORE THAN ONE TCP segment: the
// client sends the headers + part of the body, pauses, then sends the rest. The
// server must keep reading until Content-Length bytes have arrived — proven by the
// handler echoing the FULL body, not just the first segment.
func TestMachwebReadRequestMultiSegment(t *testing.T) {
	app := loopbackHelpers + `
func main() {
    port := 48231
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return ok_text("got[" + req.method + " " + req.path + "]=" + req.body) })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    // headers + the first 5 bytes of an 11-byte body...
    write(c, "POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 11\r\n\r\nHello")
    sleep(40)
    write(c, " World")   // ...the rest in a second segment
    resp := read_all(c)
    close(c)
    println(resp)
}`
	out, err := RunCaptured(machwebProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "listen-failed") || strings.Contains(out, "dial-failed") {
		t.Fatalf("loopback setup failed:\n%s", out)
	}
	if !strings.Contains(out, "200 OK") || !strings.Contains(out, "Content-Type: text/plain") {
		t.Fatalf("expected a 200 text response; got:\n%s", out)
	}
	// the full body was reassembled across both segments
	if !strings.Contains(out, "got[POST /echo]=Hello World") {
		t.Fatalf("read_request should reassemble the split body; got:\n%s", out)
	}
}

// The binary response path (is_bin=1) writes the headers then write_bytes(conn,
// bin) — NUL-safe. The body here starts with a 0x00 byte, so a text/strlen path
// would report Content-Length 0; the binary path reports the true byte count (4).
// That length in the response head is the proof the bytes path ran, not the text one.
func TestMachwebBinaryResponse(t *testing.T) {
	app := loopbackHelpers + `
func main() {
    port := 48232
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return ok_bytes("application/octet-stream", from_hex("00ff0042")) })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    write(c, "GET /blob HTTP/1.1\r\nHost: x\r\n\r\n")
    resp := read_all(c)
    close(c)
    println("HEAD<" + resp + ">")
}`
	out, err := RunCaptured(machwebProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "listen-failed") || strings.Contains(out, "dial-failed") {
		t.Fatalf("loopback setup failed:\n%s", out)
	}
	if !strings.Contains(out, "Content-Type: application/octet-stream") {
		t.Fatalf("binary response should carry its content type; got:\n%s", out)
	}
	// 4-byte body counted correctly despite the leading NUL (a text path would say 0)
	if !strings.Contains(out, "Content-Length: 4") {
		t.Fatalf("binary path should report the true byte count (4); got:\n%s", out)
	}
}
