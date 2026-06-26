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

// End-to-end cookie round-trip over the socket: the client sends a Cookie header,
// the server reads it with cookie(), verifies it as a session, and responds with a
// fresh Set-Cookie — which must appear in the raw wire response.
func TestMachwebCookieRoundTrip(t *testing.T) {
	app := loopbackHelpers + `
func handle(req) (res) {
    got := cookie(req, "sid")
    res = set_session(ok_text("saw=" + got), "secret", "sid", "user:7")
}
func main() {
    port := 48235
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return handle(req) })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    write(c, "GET / HTTP/1.1\r\nHost: x\r\nCookie: sid=incoming\r\n\r\n")
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
	// the server read the inbound cookie...
	if !strings.Contains(out, "saw=incoming") {
		t.Fatalf("server should read the inbound cookie; got:\n%s", out)
	}
	// ...and the response carries a Set-Cookie with the signed session value
	if !strings.Contains(out, "Set-Cookie: sid=user:7.") || !strings.Contains(out, "HttpOnly") {
		t.Fatalf("response should carry the signed Set-Cookie; got:\n%s", out)
	}
}

// End-to-end multipart/form-data upload: the client sends a real multipart body — a
// text field plus a "file" whose bytes include NULs — over the socket via write_bytes.
// The server reads it with the binary-safe read_request_bytes path and parse_multipart,
// then echoes the file's length + hex. The proof of binary-safety is that the 4-byte
// file (0011ff00, two NULs) round-trips intact; a NUL-truncating string path would lose
// everything from the first 0x00.
func TestMachwebMultipartUpload(t *testing.T) {
	app := loopbackHelpers + `
func handle(req) (res) {
    title := multipart_field(req, "title")
    f, ok := multipart_file(req, "file")
    res = ok_text("title=" + title + " ok=" + str(ok) + " name=" + f.filename + " ct=" + f.ctype + " len=" + str(len(f.data)) + " hex=" + to_hex(f.data))
}
func main() {
    port := 48236
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return handle(req) })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    bnd := "----mfltestB"
    pre := "--" + bnd + "\r\nContent-Disposition: form-data; name=\"title\"\r\n\r\nSME share\r\n--" + bnd + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"x.bin\"\r\nContent-Type: application/octet-stream\r\n\r\n"
    post := "\r\n--" + bnd + "--\r\n"
    body := bytes_concat(bytes_concat(bytes(pre), from_hex("0011ff00")), bytes(post))
    head := "POST /upload HTTP/1.1\r\nHost: x\r\nContent-Type: multipart/form-data; boundary=" + bnd + "\r\nContent-Length: " + str(len(body)) + "\r\n\r\n"
    write_bytes(c, bytes_concat(bytes(head), body))
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
	if !strings.Contains(out, "title=SME share") {
		t.Fatalf("server should read the text field; got:\n%s", out)
	}
	// the binary file round-tripped intact through the multipart parser, NULs and all
	if !strings.Contains(out, "ok=1 name=x.bin ct=application/octet-stream len=4 hex=0011ff00") {
		t.Fatalf("multipart file should round-trip binary-safe (len=4, hex=0011ff00); got:\n%s", out)
	}
}

// Proxy-awareness: behind a TLS-terminating proxy the socket is plain HTTP, but
// X-Forwarded-Proto/For carry the real scheme + client IP. With set_trust_proxy(1) the
// handler sees scheme=https, the forwarded client IP, an https base_url, and cookies are
// marked Secure. Proven over the socket with forged X-Forwarded-* headers.
func TestMachwebProxyAwareness(t *testing.T) {
	app := loopbackHelpers + `
func handle(req) (res) {
    res = set_cookie(ok_text("scheme=" + scheme(req) + " ip=" + client_ip(req) + " base=" + base_url(req)), "sid", "x")
}
func main() {
    set_trust_proxy(1)
    set_secure_cookies(1)
    port := 48238
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return handle(req) })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    write(c, "GET /acct HTTP/1.1\r\nHost: app.example\r\nX-Forwarded-Proto: https\r\nX-Forwarded-For: 203.0.113.7, 10.0.0.1\r\n\r\n")
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
	if !strings.Contains(out, "scheme=https") {
		t.Fatalf("scheme should come from X-Forwarded-Proto; got:\n%s", out)
	}
	if !strings.Contains(out, "ip=203.0.113.7") {
		t.Fatalf("client_ip should be the left-most X-Forwarded-For hop; got:\n%s", out)
	}
	if !strings.Contains(out, "base=https://app.example") {
		t.Fatalf("base_url should be scheme://host; got:\n%s", out)
	}
	if !strings.Contains(out, "Set-Cookie: sid=x; Path=/; HttpOnly; SameSite=Lax; Secure") {
		t.Fatalf("cookies should be marked Secure when set_secure_cookies(1); got:\n%s", out)
	}
}

// The request body cap: with set_max_body(N), a request declaring a larger body is
// rejected 413 without buffering it all.
func TestMachwebMaxBody(t *testing.T) {
	app := loopbackHelpers + `
func main() {
    set_max_body(100)
    port := 48239
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return ok_text("ok") })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    write(c, "POST /up HTTP/1.1\r\nHost: x\r\nContent-Length: 5000\r\n\r\n" + "AAAAAAAAAA")
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
	if !strings.Contains(out, "413 Payload Too Large") {
		t.Fatalf("an over-cap body should be rejected 413; got:\n%s", out)
	}
}

// A streaming (SSE) response: the handler returns sse(fn), and machweb writes the
// event-stream headers (no Content-Length) then lets fn write data events over the open
// socket. The client reads until the server closes. Proven by the text/event-stream
// content type + the three "data:" events arriving in the body.
func TestMachwebSSEStream(t *testing.T) {
	app := loopbackHelpers + `
func emit(conn) {
    i := 0
    while i < 3 {
        n := sse_data(conn, "tick " + str(i))
        if n < 0 { return }
        i = i + 1
    }
}
func handle(req) (res) {
    if req.path == "/events" { res = sse(func(conn) { emit(conn) })  return }
    res = ok_text("home")
}
func main() {
    port := 48237
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go serve_one(srv, func(req) { return handle(req) })
    sleep(50)
    c := dial("127.0.0.1", port)
    if c < 0 { println("dial-failed")  return }
    write(c, "GET /events HTTP/1.1\r\nHost: x\r\n\r\n")
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
	if !strings.Contains(out, "Content-Type: text/event-stream") {
		t.Fatalf("SSE response should carry the event-stream content type; got:\n%s", out)
	}
	// a streaming response has no Content-Length, and all three events arrived
	if strings.Contains(out, "Content-Length:") {
		t.Fatalf("a streaming response must not send Content-Length; got:\n%s", out)
	}
	if !strings.Contains(out, "data: tick 0") || !strings.Contains(out, "data: tick 1") || !strings.Contains(out, "data: tick 2") {
		t.Fatalf("all three SSE events should stream through; got:\n%s", out)
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
