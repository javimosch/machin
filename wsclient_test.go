package main

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func wsclientProg(t *testing.T, app string) *Program {
	t.Helper()
	src, err := os.ReadFile("framework/wsclient.src")
	if err != nil {
		t.Skip("framework/wsclient.src not found")
	}
	prog, perr := progFromSrcErr(string(src) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// wsReadHTTPHeaders reads a client's HTTP request through the blank line and
// returns it. The MFL client sends nothing after the headers until it sees the
// 101, so reading to EOF-of-headers can never swallow frame bytes.
func wsReadHTTPHeaders(c net.Conn) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(c, buf); err != nil {
			return "", err
		}
		b.WriteByte(buf[0])
		s := b.String()
		if strings.HasSuffix(s, "\r\n\r\n") {
			return s, nil
		}
		if b.Len() > 65536 {
			return "", fmt.Errorf("handshake headers too large")
		}
	}
}

// wsReadFrame reads one RFC 6455 frame. Client frames are masked; the payload is
// unmasked before return. Server frames this test writes are unmasked.
func wsReadFrame(c net.Conn) (fin bool, opcode byte, payload []byte, err error) {
	hdr := make([]byte, 2)
	if _, err = io.ReadFull(c, hdr); err != nil {
		return
	}
	fin = hdr[0]&0x80 != 0
	opcode = hdr[0] & 0x0f
	n := int64(hdr[1] & 0x7f)
	masked := hdr[1]&0x80 != 0
	if n == 126 {
		ext := make([]byte, 2)
		if _, err = io.ReadFull(c, ext); err != nil {
			return
		}
		n = int64(ext[0])<<8 | int64(ext[1])
	} else if n == 127 {
		ext := make([]byte, 8)
		if _, err = io.ReadFull(c, ext); err != nil {
			return
		}
		n = 0
		for _, b := range ext {
			n = n<<8 | int64(b)
		}
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return
}

// wsWriteFrame writes one unmasked server->client frame.
func wsWriteFrame(c net.Conn, opcode byte, payload []byte) error {
	return wsWriteFrameFin(c, true, opcode, payload)
}

// wsWriteFrameFin is wsWriteFrame with an explicit FIN bit (for fragments).
func wsWriteFrameFin(c net.Conn, fin bool, opcode byte, payload []byte) error {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	hdr := []byte{b0}
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 65536:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	if _, err := c.Write(append(hdr, payload...)); err != nil {
		return err
	}
	return nil
}

func wsAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// wsHandshake reads the client's upgrade request, verifies it is a well-formed
// ws:// handshake for path, and answers 101 with the correct accept key.
func wsHandshake(c net.Conn, path string) error {
	req, err := wsReadHTTPHeaders(c)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(req, "GET "+path+" HTTP/1.1\r\n") {
		return fmt.Errorf("bad request line: %q", req)
	}
	var key string
	for _, line := range strings.Split(req, "\r\n") {
		if strings.HasPrefix(line, "Sec-WebSocket-Key: ") {
			key = strings.TrimPrefix(line, "Sec-WebSocket-Key: ")
		}
	}
	if key == "" {
		return fmt.Errorf("no Sec-WebSocket-Key in %q", req)
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAcceptKey(key) + "\r\n\r\n"
	_, err = c.Write([]byte(resp))
	return err
}

// wsEchoSession plays the server side of one connection: echoes text and binary
// messages, injects one ping before the first text echo (to exercise the client's
// auto-pong), and records the client's pong until it closes.
func wsEchoSession(c net.Conn, pongSeen *int32) {
	defer c.Close()
	pinged := false
	for {
		fin, op, pl, err := wsReadFrame(c)
		if err != nil || !fin {
			return
		}
		switch op {
		case 1: // text -> ping once, then a FRAGMENTED echo (FIN=0 + continuation)
			if !pinged {
				pinged = true
				wsWriteFrame(c, 9, []byte("pp"))
			}
			echo := append([]byte("echo:"), pl...)
			cut := len(echo) / 2
			wsWriteFrameFin(c, false, 1, echo[:cut])
			wsWriteFrameFin(c, true, 0, echo[cut:])
		case 2: // binary -> echo back verbatim
			wsWriteFrame(c, 2, pl)
		case 10:
			atomic.StoreInt32(pongSeen, 1)
		case 8:
			return
		}
	}
}

// A full ws:// round-trip through framework/wsclient.src against an independent
// Go server: masked client frames are verified by unmasking, the HTTP upgrade is
// checked (request line + accept key), the text echo arrives FRAGMENTED (exercising
// reassembly), ping->pong is exercised, binary payloads round-trip NUL-safely at
// both short and extended (>125 byte) lengths, and wsc_open_h's extra headers
// reach the handshake.
func TestWSClientRoundTrip(t *testing.T) {
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

		// connection 2: wsc_open_h header check, then close
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
    c, ok := wsc_open("127.0.0.1", %d, "/chat")
    if ok == 0 { println("OPEN-FAIL")  return }
    n := wsc_send(c, "hello")
    msg, good := wsc_recv(c)
    println("GOT ok=" + str(good) + " msg=[" + msg + "]")
    n2 := wsc_send_bin(c, from_hex("00ff10"))
    bin, bok := wsc_recv_bin(c)
    println("BIN ok=" + str(bok) + " len=" + str(len(bin)) + " hex=" + to_hex(bin))
    big := ""
    i := 0
    while i < 200 { big = big + "x"  i = i + 1 }
    wsc_send_bin(c, bytes(big))
    big2, bok2 := wsc_recv_bin(c)
    println("BIG ok=" + str(bok2) + " len=" + str(len(big2)))
    wsc_close(c)
    c2, ok2 := wsc_open_h("127.0.0.1", %d, "/chat", []string{"Authorization: Bearer sekret", "X-Custom: yes"})
    if ok2 == 1 { wsc_close(c2) }
    println("DONE ok2=" + str(ok2))
}`, port, port)

	out, err := RunCaptured(wsclientProg(t, app))
	if err != nil {
		t.Fatalf("run: %v\nstderr-out:\n%s", err, out)
	}
	if strings.Contains(out, "OPEN-FAIL") {
		t.Fatalf("wsc_open should succeed against a real ws:// server; got:\n%s", out)
	}
	if !strings.Contains(out, "GOT ok=1 msg=[echo:hello]") {
		t.Fatalf("text echo round-trip failed; got:\n%s", out)
	}
	if !strings.Contains(out, "BIN ok=1 len=3 hex=00ff10") {
		t.Fatalf("binary round-trip failed (NUL-safety); got:\n%s", out)
	}
	if !strings.Contains(out, "BIG ok=1 len=200") {
		t.Fatalf("200-byte binary round-trip failed (extended payload length); got:\n%s", out)
	}
	if !strings.Contains(out, "DONE ok2=1") {
		t.Fatalf("wsc_open_h should succeed with extra headers; got:\n%s", out)
	}
	if atomic.LoadInt32(&pongSeen) != 1 {
		t.Fatalf("server never saw the client's pong reply to its ping")
	}
	if atomic.LoadInt32(&authSeen) != 1 {
		t.Fatalf("server never saw the wsc_open_h extra headers")
	}
}
