package main

import (
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
)

// The ws_* builtins (#694) are the cleartext sibling of wss_*: same handle
// shape and frame codec, but the transport is a plain dial() fd — no OpenSSL.
func TestWSRuntimeGating(t *testing.T) {
	prog := &Program{Funcs: parseFuncs(t, `func main() {
		c := ws_open("ws://x")
		ws_send(c, "hi")
		println(ws_recv(c))
		ws_close(c)
	}`)}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_ws_open(") || !strings.Contains(c, "mfl_ws_send(") || !strings.Contains(c, "mfl_ws_recv(") || !strings.Contains(c, "mfl_ws_close(") {
		t.Fatal("a program using ws_* must emit the mfl_ws_* entry points")
	}
	if strings.Contains(c, "openssl/ssl.h") || strings.Contains(c, "mfl_wss_") || strings.Contains(c, "mfl_tls_dial") {
		t.Fatal("a ws://-only program must not emit the TLS or WSS runtimes")
	}
	if !strings.Contains(c, "mfl_dial(") {
		t.Fatal("ws_open must build on the plain TCP dial runtime")
	}

	// A program that never touches ws_* must not emit the runtime.
	plain := &Program{Funcs: parseFuncs(t, `func main() { println("hi") }`)}
	c2, err := CompileToC(plain, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c2, "mfl_ws_") || strings.Contains(c2, "mfl_wst_") {
		t.Fatal("a program with no ws_* calls must not emit the cleartext WebSocket runtime")
	}
}

// ws_open rejects non-ws:// schemes and CR/LF-bearing header lines without
// dialling; the 2-arg form emits mfl_ws_open_h.
func TestWSOpenArgForms(t *testing.T) {
	prog := &Program{Funcs: parseFuncs(t, `func main() {
		a := ws_open("wss://x")
		b := ws_open("ws://x", []string{"Authorization: Bearer t"})
		c := ws_open("ws://x", []string{"X: 1\r\nInjected: 2"})
		println(a + b + c)
	}`)}
	out, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mfl_ws_open(") || !strings.Contains(out, "mfl_ws_open_h(") {
		t.Fatal("ws_open must emit mfl_ws_open / mfl_ws_open_h")
	}
	if strings.Contains(out, "openssl") {
		t.Fatal("ws_open must not pull in OpenSSL even for a wss:// URL — it refuses it")
	}
}

// A full ws:// round-trip through the ws_* builtins against an independent Go
// server (the same harness wsclient_test.go uses for framework/wsclient.src):
// masked client frames verified by unmasking, the HTTP upgrade checked, the
// text echo arrives fragmented (reassembly), ping->pong is exercised, binary
// payloads round-trip NUL-safely at short and extended lengths, ws_open's
// extra headers reach the handshake, and a CR/LF header line is refused (0,
// nothing sent — the server never sees a third connection).
func TestWSBuiltinRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	var pongSeen, authSeen int32
	go func() {
		// connection 1: the echo/ping/pong/binary session
		c1, err := ln.Accept()
		if err != nil {
			return
		}
		if err := wsHandshake(c1, "/chat"); err != nil {
			c1.Close()
			return
		}
		wsEchoSession(c1, &pongSeen)

		// connection 2: ws_open's header check, then close
		c2, err := ln.Accept()
		if err != nil {
			return
		}
		req, err := wsReadHTTPHeaders(c2)
		if err != nil {
			c2.Close()
			return
		}
		var key string
		for _, line := range strings.Split(req, "\r\n") {
			if strings.HasPrefix(line, "Sec-WebSocket-Key: ") {
				key = strings.TrimPrefix(line, "Sec-WebSocket-Key: ")
			}
		}
		if key == "" || !strings.HasPrefix(req, "GET /chat HTTP/1.1\r\n") {
			c2.Close()
			return
		}
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + wsAcceptKey(key) + "\r\n\r\n"
		c2.Write([]byte(resp))
		if strings.Contains(req, "Authorization: Bearer sekret") && strings.Contains(req, "X-Custom: yes") {
			atomic.StoreInt32(&authSeen, 1)
		}
		for {
			_, op, _, err := wsReadFrame(c2)
			if err != nil || op == 8 {
				break
			}
		}
		c2.Close()
	}()

	app := fmt.Sprintf(`
func main() {
    h := ws_open("ws://127.0.0.1:%d/chat")
    if h == 0 { println("OPEN-FAIL")  return }
    ws_send(h, "hello")
    msg := ws_recv(h)
    println("GOT msg=[" + msg + "]")
    ws_send_bin(h, from_hex("00ff10"))
    bin := ws_recv_bin(h)
    println("BIN len=" + str(len(bin)) + " hex=" + to_hex(bin))
    big := ""
    i := 0
    while i < 200 { big = big + "x"  i = i + 1 }
    ws_send_bin(h, bytes(big))
    big2 := ws_recv_bin(h)
    println("BIG len=" + str(len(big2)))
    ws_close(h)
    h2 := ws_open("ws://127.0.0.1:%d/chat", []string{"Authorization: Bearer sekret", "X-Custom: yes"})
    if h2 != 0 { ws_close(h2) }
    h3 := ws_open("ws://127.0.0.1:%d/chat", []string{"X: 1\r\nInjected: 2"})
    println("DONE h2=" + str(h2) + " h3=" + str(h3))
}`, port, port, port)

	prog, perr := progFromSrcErr(app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v\nstderr-out:\n%s", err, out)
	}
	if strings.Contains(out, "OPEN-FAIL") {
		t.Fatalf("ws_open should succeed against a real ws:// server; got:\n%s", out)
	}
	if !strings.Contains(out, "GOT msg=[echo:hello]") {
		t.Fatalf("text echo round-trip failed; got:\n%s", out)
	}
	if !strings.Contains(out, "BIN len=3 hex=00ff10") {
		t.Fatalf("binary round-trip failed (NUL-safety); got:\n%s", out)
	}
	if !strings.Contains(out, "BIG len=200") {
		t.Fatalf("200-byte binary round-trip failed (extended payload length); got:\n%s", out)
	}
	if !strings.Contains(out, "DONE h2=") || !strings.Contains(out, "h3=0") {
		t.Fatalf("ws_open headers form should succeed (h2!=0) and the CR/LF line must be refused (h3=0); got:\n%s", out)
	}
	if atomic.LoadInt32(&pongSeen) != 1 {
		t.Fatalf("server never saw the client's pong reply to its ping")
	}
	if atomic.LoadInt32(&authSeen) != 1 {
		t.Fatalf("server never saw the ws_open extra headers")
	}
}
