package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startWSServer builds machweb + ws.src + app, runs it on a free loopback port
// and returns the address and the process id.
func startWSServer(t *testing.T, app string) (string, int) {
	t.Helper()
	port := freeLoopbackPort(t)
	prog := wsProg(t, app+fmt.Sprintf(`
func main() { serve_on("127.0.0.1", %d, func(req) { return handle(req) }) }
`, port))
	bin := filepath.Join(t.TempDir(), "wsserver")
	if err := BuildBinary(prog, bin, false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(bin)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 200; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return addr, cmd.Process.Pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not start")
	return "", 0
}

type wsPeer struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

func dialWS(t *testing.T, addr string) *wsPeer {
	t.Helper()
	return dialWSPath(t, addr, "/")
}

func dialWSPath(t *testing.T, addr, path string) *wsPeer {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(30 * time.Second))
	io.WriteString(c, "GET "+path+" HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	r := bufio.NewReader(c)
	resp, err := http.ReadResponse(r, nil)
	if err != nil || resp.StatusCode != 101 {
		t.Fatalf("upgrade failed: %v %v", resp, err)
	}
	return &wsPeer{t, c, r}
}

// wsClientFrame builds a client frame; first is the FIN/RSV/opcode byte.
func wsClientFrame(first byte, payload []byte, masked bool) []byte {
	f := []byte{first}
	m := byte(0)
	if masked {
		m = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		f = append(f, m|byte(n))
	case n < 65536:
		f = binary.BigEndian.AppendUint16(append(f, m|126), uint16(n))
	default:
		f = binary.BigEndian.AppendUint64(append(f, m|127), uint64(n))
	}
	if !masked {
		return append(f, payload...)
	}
	key := []byte{7, 0, 200, 13}
	f = append(f, key...)
	for i, b := range payload {
		f = append(f, b^key[i%4])
	}
	return f
}

func (p *wsPeer) send(first byte, payload string) {
	p.c.Write(wsClientFrame(first, []byte(payload), true))
}

func wsEnded(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "eof"
}

// read returns the next server frame as "opcode:payload"; a close frame is
// "close:<status code>", and a connection that ended is "eof".
func (p *wsPeer) read() string {
	h := make([]byte, 2)
	if _, err := io.ReadFull(p.r, h); err != nil {
		return wsEnded(err)
	}
	n := int(h[1] & 127)
	if n >= 126 {
		e := make([]byte, 2)
		if n == 127 {
			e = make([]byte, 8)
		}
		io.ReadFull(p.r, e)
		n = 0
		for _, b := range e {
			n = n<<8 | int(b)
		}
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(p.r, body); err != nil {
		return wsEnded(err)
	}
	if h[0]&15 == 8 {
		if len(body) < 2 {
			return "close:"
		}
		return fmt.Sprintf("close:%d", binary.BigEndian.Uint16(body))
	}
	return fmt.Sprintf("%d:%s", h[0]&15, body)
}

func (p *wsPeer) expect(want string) {
	p.t.Helper()
	if got := p.read(); got != want {
		p.t.Fatalf("got %.100q, want %.100q", got, want)
	}
}

const wsMessageApp = `
func handle(req) (res) {
    res = ws(req, func(c) {
        ok := 1
        while ok == 1 {
            arena {
                op, data, got := ws_next_message(c)
                ok = got
                if got == 1 { n := ws_send_text(c.fd, str(op) + " " + to_hex(data)) }
            }
        }
        // once finished, a connection stays finished
        op2, data2, again := ws_next_message(c)
        if again == 1 { n := ws_send_text(c.fd, "read after the end") }
    })
}
`

func TestWebSocketServerProtocol(t *testing.T) {
	addr, _ := startWSServer(t, wsMessageApp)

	p := dialWS(t, addr)
	p.send(0x81, "hi")
	p.expect("1:1 6869")
	// fragments make one message, with a ping answered in between
	p.send(0x01, "Hel")
	p.send(0x89, "?")
	p.expect("10:?")
	p.send(0x00, "l")
	p.send(0x80, "o")
	p.expect("1:1 48656c6c6f")
	// many fragments reassemble in order
	p.send(0x02, "a")
	for i := 0; i < 40; i++ {
		p.send(0x00, string(rune('b'+i%20)))
	}
	p.send(0x80, "z")
	want := "a"
	for i := 0; i < 40; i++ {
		want += string(rune('b' + i%20))
	}
	p.expect(fmt.Sprintf("1:2 %x", want+"z"))
	// binary data is NUL-safe through ws_next_message; empty messages work
	p.send(0x82, "\x00\xff\x00")
	p.expect("1:2 00ff00")
	p.send(0x81, "")
	p.expect("1:1 ")
	p.send(0x88, "")
	if got := p.read(); got != "eof" {
		t.Fatalf("after a close frame: %q", got)
	}

	violations := map[string][]byte{
		"unmasked":             wsClientFrame(0x81, []byte("x"), false),
		"reserved bit":         wsClientFrame(0xC1, []byte("x"), true),
		"reserved opcode":      wsClientFrame(0x83, []byte("x"), true),
		"lone continuation":    wsClientFrame(0x80, []byte("x"), true),
		"message inside other": append(wsClientFrame(0x01, []byte("a"), true), wsClientFrame(0x81, []byte("b"), true)...),
		"fragmented ping":      wsClientFrame(0x09, []byte("x"), true),
		"long ping":            wsClientFrame(0x89, make([]byte, 126), true),
	}
	for name, frame := range violations {
		p := dialWS(t, addr)
		p.c.Write(frame)
		if got := p.read(); got != "close:1002" {
			t.Errorf("%s: got %q, want close:1002", name, got)
		}
	}
}

// A WSConn built with ws_conn (not by ws()) can read a server's unmasked frames,
// which is how programs use ws.src as a client.
func TestWebSocketOwnConnReadsUnmaskedFrames(t *testing.T) {
	addr := rawHTTPServer(t, func(string) (string, bool) {
		return "\x81\x02hi\x01\x01a\x80\x01b", false
	})
	port := addr[strings.LastIndex(addr, ":")+1:]
	out, err := RunCaptured(wsProg(t, `
func main() {
    fd := dial("127.0.0.1", `+port+`)
    n := write(fd, "go\r\n\r\n")
    c := ws_conn(fd)
    for {
        msg, ok := ws_next_text(c)
        if ok == 0 { break }
        println(msg)
    }
}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "hi\nab\n" {
		t.Errorf("got %q", out)
	}
}

func TestWebSocketNextTextNeverTruncates(t *testing.T) {
	addr, _ := startWSServer(t, `
func handle(req) (res) {
    res = ws(req, func(c) {
        for {
            msg, ok := ws_next_text(c)
            if ok == 0 { return }
            n := ws_send_text(c.fd, "got " + msg)
        }
    })
}
`)
	p := dialWS(t, addr)
	p.send(0x82, "binary without a zero byte")
	p.expect("1:got binary without a zero byte")
	p.send(0x01, "spl")
	p.send(0x80, "it")
	p.expect("1:got split")
	p.send(0x81, "a\x00b")
	p.expect("close:1003")
	if got := p.read(); strings.HasPrefix(got, "1:") {
		t.Errorf("a message with a zero byte was delivered: %q", got)
	}
}
