package main

// Regression tests for #733 (HTTP client credentials and request line) and #734
// (WebSocket resource exhaustion).

import (
	"bytes"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// releasePeakRSS undoes what multi-MiB buffers in the test process do to later
// tests: on Linux a child ru_maxrss starts from its parent peak resident size,
// which breaks the arena RSS-ratio tests. Writing 5 to clear_refs resets it.
func releasePeakRSS() {
	debug.FreeOSMemory()
	_ = os.WriteFile("/proc/self/clear_refs", []byte("5"), 0)
}

type headLog struct {
	mu    sync.Mutex
	heads []string
}

func (l *headLog) add(s string) { l.mu.Lock(); l.heads = append(l.heads, s); l.mu.Unlock() }
func (l *headLog) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.heads, "\n----\n")
}

func TestHTTPRedirectKeepsCredentialsOnlyOnSameOrigin(t *testing.T) {
	var landed headLog
	other := rawHTTPServer(t, func(head string) (string, bool) {
		landed.add(head)
		return "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nother", false
	})
	_, otherPort, _ := net.SplitHostPort(other)
	var same headLog
	first := rawHTTPServer(t, func(head string) (string, bool) {
		path := strings.Fields(head)[1]
		to := func(code int, loc string) string {
			return fmt.Sprintf("HTTP/1.1 %d X\r\nLocation: %s\r\nContent-Length: 0\r\n\r\n", code, loc)
		}
		switch path {
		case "/other-port":
			return to(302, "http://"+other+"/a"), false
		case "/other-host":
			return to(302, "http://localhost:"+otherPort+"/b"), false
		case "/back-and-forth":
			return to(307, "http://"+other+"/c"), false
		case "/same":
			return to(302, "/stay"), false
		}
		same.add(head)
		return "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nsame", false
	})
	var src strings.Builder
	src.WriteString(`func main() {
    h := []string{"Authorization: Bearer S3CRET", "cookie: sid=S3CRET", "Proxy-Authorization: S3CRET", "X-Api-Key: S3CRET", "Host: S3CRET", "Accept-Language: fr", "Content-Type: text/plain"}
`)
	for _, p := range []string{"/other-port", "/other-host", "/back-and-forth", "/same"} {
		fmt.Fprintf(&src, "    s%[1]d, b%[1]d, e%[1]d := http_request(\"GET\", \"http://%[2]s%[3]s\", h, \"\")\n    println(str(s%[1]d) + b%[1]d + e%[1]d)\n", len(src.String()), first, p)
	}
	src.WriteString("}\n")
	out, err := RunCaptured(progFromSrc(t, src.String()))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "200other\n200other\n200other\n200same\n" {
		t.Fatalf("output: %q", out)
	}
	if got := landed.all(); strings.Contains(got, "S3CRET") || strings.Count(got, "Accept-Language: fr") != 3 || strings.Count(got, "Content-Type: text/plain") != 3 {
		t.Errorf("headers sent to another origin:\n%s", got)
	}
	if got := same.all(); !strings.Contains(got, "Bearer S3CRET") || !strings.Contains(got, "X-Api-Key: S3CRET") {
		t.Errorf("same-origin redirect lost headers:\n%s", got)
	}
}

func TestHTTPSToHTTPRedirectDropsCredentialsAndBody(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses SSL_CERT_FILE")
	}
	var landed headLog
	plain := rawHTTPServer(t, func(head string) (string, bool) {
		landed.add(head)
		return "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nplain", false
	})
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := 302
		if r.URL.Path == "/keep-body" {
			code = 307
		}
		w.Header().Set("Location", "http://"+plain+r.URL.Path)
		w.WriteHeader(code)
	}))
	defer ts.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", ca)
	out, err := RunCaptured(progFromSrc(t, fmt.Sprintf(`func main() {
    h := []string{"Authorization: Bearer S3CRET"}
    s, b, e := http_request("GET", "%[1]s/get", h, "")
    println(str(s) + "|" + b + "|" + e)
    s2, b2, e2 := http_request("POST", "%[1]s/keep-body", h, "password=S3CRET")
    println(str(s2) + "|" + b2 + "|" + e2)
}`, ts.URL)))
	if err != nil {
		if strings.Contains(err.Error(), "ssl") {
			t.Skipf("OpenSSL unavailable: %v", err)
		}
		t.Fatalf("run: %v", err)
	}
	if out != "200|plain|\n307||\n" {
		t.Fatalf("output: %q", out)
	}
	if got := landed.all(); strings.Contains(got, "S3CRET") || strings.Contains(got, "keep-body") {
		t.Errorf("sent in cleartext:\n%s", got)
	}
}

func TestClientRequestLineInjectionRefused(t *testing.T) {
	var seen headLog
	addr := rawHTTPServer(t, func(head string) (string, bool) {
		seen.add(head)
		return "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok", false
	})
	_, port, _ := net.SplitHostPort(addr)
	hs, err := os.ReadFile("framework/httpstream.src")
	if err != nil {
		t.Fatal(err)
	}
	wsc, err := os.ReadFile("framework/wsclient.src")
	if err != nil {
		t.Fatal(err)
	}
	app := string(hs) + string(wsc) + `
func req(m, u) {
    s, b, e := http_request(m, u, []string{}, "")
    println(str(s) + "|" + e)
}
func main() {
    base := "http://` + addr + `"
    bad := "/x HTTP/1.1\r\nX-Injected: 1\r\nY: z"
    req("GET", base + bad)
    req("GET", base + "/a\n")
    req("GET", "http://127.0.0.1\r\nX-Injected: 1:` + port + `/")
    req("GET /m HTTP/1.1\r\nX-Injected: 1\r\nZ:", base + "/y")
    req("GE T", base + "/y")
    req("", base + "/y")
    s, b, e := http_get(base + bad)
    println(str(s) + "|" + e)
    println("[" + https_get(base + bad) + https_post(base + bad, "x") + "]")
    println(ws_open("ws://` + addr + `" + bad))
    println(ws_open("ws://` + addr + `" + bad, []string{}))
    println(wss_open("wss://` + addr + `" + bad))
    h, ok := hs_open("GET", base + bad, []string{}, "")
    println(ok)
    h2, ok2 := hs_open("GET " + bad, base + "/", []string{}, "")
    println(ok2)
    c, ok3 := wsc_open("127.0.0.1", ` + port + `, bad)
    println(ok3)
    for _, hex := range []string{"0b", "0c", "7f", "01"} {
        ctl := "/a" + bytes_str(from_hex(hex))
        h3, ok4 := hs_open("GET", base + ctl, []string{}, "")
        c2, ok5 := wsc_open("127.0.0.1", ` + port + `, ctl)
        println(str(ok4) + str(ok5))
    }
}
`
	prog, perr := progFromSrcErr(app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		if strings.Contains(err.Error(), "ssl") {
			t.Skipf("OpenSSL unavailable: %v", err)
		}
		t.Fatalf("run: %v", err)
	}
	want := "0|url\n0|url\n0|url\n0|method\n0|method\n0|method\n0|url\n[]\n0\n0\n0\n0\n0\n0\n00\n00\n00\n00\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
	if got := seen.all(); got != "" {
		t.Errorf("a refused request reached the server:\n%s", got)
	}
}

// declare builds just the header of a masked text frame claiming n payload bytes.
func declare(first byte, n uint64) []byte {
	return binary.BigEndian.AppendUint64([]byte{first, 0x80 | 127}, n)
}

func TestWebSocketServerMessageLimit(t *testing.T) {
	t.Cleanup(releasePeakRSS)
	addr, _ := startWSServer(t, wsMessageApp)
	for name, frame := range map[string][]byte{
		"declared 1 TiB":     declare(0x82, 1<<40),
		"declared maxint":    declare(0x82, 1<<63-1),
		"one over default":   declare(0x82, 16<<20+1),
		"sum over the limit": append(wsClientFrame(0x02, make([]byte, 70000), true), declare(0x80, 16<<20-60000)...),
	} {
		p := dialWS(t, addr)
		p.c.Write(frame) // no payload follows: the refusal must not wait for it
		if got := p.read(); got != "close:1009" {
			t.Errorf("%s: got %q, want close:1009", name, got)
		}
	}
	p := dialWS(t, addr)
	p.c.Write(declare(0x82, 1<<63))
	if got := p.read(); got != "close:1002" {
		t.Errorf("negative length: got %q", got)
	}
	p = dialWS(t, addr)
	var flood []byte
	flood = append(flood, wsClientFrame(0x02, nil, true)...)
	for i := 0; i < 4096; i++ {
		flood = append(flood, wsClientFrame(0x00, nil, true)...)
	}
	p.c.Write(flood)
	if got := p.read(); got != "close:1009" {
		t.Errorf("fragment flood: got %q", got)
	}
}

func TestWebSocketServerLimitIsConfigurable(t *testing.T) {
	addr, _ := startWSServer(t, `
func handle(req) (res) {
    ws_set_max_message(8)
    res = ws(req, func(c) {
        for {
            msg, ok := ws_next_text(c)
            if ok == 0 { return }
            n := ws_send_text(c.fd, msg)
        }
    })
}
`)
	p := dialWS(t, addr)
	p.send(0x81, "12345678")
	p.expect("1:12345678")
	p.send(0x81, "123456789")
	p.expect("close:1009")
}

func peakRSSKB(t *testing.T, pid int) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Skip("no /proc")
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) > 1 && f[0] == "VmHWM:" {
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return 0
}

func TestWebSocketServerMemoryIsLinear(t *testing.T) {
	t.Cleanup(releasePeakRSS)
	addr, pid := startWSServer(t, `
func handle(req) (res) {
    res = ws(req, func(c) {
        ok := 1
        while ok == 1 {
            arena {
                msg, got := ws_next_text(c)
                ok = got
                if got == 1 { n := ws_send_text(c.fd, str(len(msg))) }
            }
        }
    })
}
`)
	p := dialWS(t, addr)
	big := strings.Repeat("a", 4<<20)
	for i := 0; i < 20; i++ { // 80 MiB in all
		p.send(0x81, big)
		p.expect("1:" + strconv.Itoa(len(big)))
	}
	// fragmented, then a stream of pings: nothing may accumulate
	p.send(0x01, "")
	for i := 0; i < 1000; i++ {
		p.send(0x00, big[:4096])
	}
	p.send(0x80, "")
	p.expect("1:4096000")
	ping := wsClientFrame(0x89, []byte(big[:125]), true)
	go func() {
		for i := 0; i < 50000; i++ {
			p.c.Write(ping)
		}
		p.send(0x81, "end")
	}()
	for i := 0; i < 50000; i++ {
		p.expect("10:" + big[:125])
	}
	p.expect("1:3")
	peak := peakRSSKB(t, pid)
	t.Logf("peak RSS %d KB", peak)
	if peak > 200*1024 {
		t.Errorf("peak RSS %d KB for 4 MiB messages", peak)
	}
}

// Control frames with no data message between them all land in one
// ws_next_message call; none of what reading them allocates may stay.
func TestWebSocketControlFloodIsFlat(t *testing.T) {
	addr, pid := startWSServer(t, wsMessageApp)
	p := dialWS(t, addr)
	batch := bytes.Repeat(wsClientFrame(0x8A, nil, true), 10000)
	for i := 0; i < 300; i++ {
		p.c.Write(batch)
	}
	p.send(0x81, "x")
	p.expect("1:1 78")
	peak := peakRSSKB(t, pid)
	t.Logf("peak RSS %d KB", peak)
	if peak > 40*1024 {
		t.Errorf("peak RSS %d KB after 3,000,000 control frames", peak)
	}
}

// A ping between two fragments does not count against the message limit.
func TestWebSocketClientLimitIgnoresControlFrames(t *testing.T) {
	addr := rawHTTPServer(t, func(string) (string, bool) {
		return "HTTP/1.1 101 Switching Protocols\r\n\r\n\x02\x04abcd\x89\x01p\x80\x00", true
	})
	t.Setenv("MFL_WS_MAX_MESSAGE", "4")
	out, err := RunCaptured(progFromSrc(t, `func main() {
    h := ws_open("ws://`+addr+`/")
    println(bytes_str(ws_recv_bin(h)))
}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "abcd\n" {
		t.Errorf("got %q", out)
	}
}

// A reserved opcode must not be taken for a control frame (exempt from the
// message limit) and then appended to the message.
func TestWebSocketClientRefusesReservedOpcodes(t *testing.T) {
	for _, op := range []byte{0x03, 0x07, 0x0B, 0x0F} {
		frames := string([]byte{op, 4}) + "aaaa" + string([]byte{op, 4}) + "bbbb" + "\x80\x00"
		addr := rawHTTPServer(t, func(string) (string, bool) {
			return "HTTP/1.1 101 Switching Protocols\r\n\r\n" + frames, true
		})
		t.Setenv("MFL_WS_MAX_MESSAGE", "4")
		out, err := RunCaptured(progFromSrc(t, `func main() {
    h := ws_open("ws://`+addr+`/")
    println(len(ws_recv_bin(h)))
}`))
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if out != "0\n" {
			t.Errorf("opcode %#x: got %q bytes", op, out)
		}
	}
}

func TestWebSocketClientMessageLimit(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	frames := [][]byte{
		binary.BigEndian.AppendUint64([]byte{0x82, 127}, 1<<62),
		binary.BigEndian.AppendUint64([]byte{0x82, 127}, 1<<64-1),
		append(append([]byte{0x02, 60}, make([]byte, 60)...), append([]byte{0x80, 60}, make([]byte, 60)...)...),
		append([]byte{0x81, 100}, []byte(strings.Repeat("k", 100))...),
	}
	go func() {
		for _, f := range frames {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn, f []byte) {
				defer c.Close()
				c.SetDeadline(time.Now().Add(10 * time.Second))
				buf := make([]byte, 4096)
				c.Read(buf)
				io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\n\r\n")
				c.Write(f)
				c.Read(buf)
			}(c, f)
		}
	}()
	t.Setenv("MFL_WS_MAX_MESSAGE", "100")
	out, err := RunCaptured(progFromSrc(t, `func one() {
    h := ws_open("ws://`+ln.Addr().String()+`/")
    println(len(ws_recv_bin(h)))
}
func main() {
    one()
    one()
    one()
    one()
}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "0\n0\n0\n100\n" {
		t.Errorf("got %q", out)
	}
}

func TestWSClientFrameworkLargeMessage(t *testing.T) {
	t.Cleanup(releasePeakRSS)
	addr, pid := startWSServer(t, `
func handle(req) (res) {
    res = ws(req, func(c) {
        op, data, ok := ws_next_message(c)
        if ok == 1 { n := ws_send_bytes(c.fd, data) }
    })
}
`)
	wsc, err := os.ReadFile("framework/wsclient.src")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(addr)
	prog, perr := progFromSrcErr(string(wsc) + `
func main() {
    s := "0123456789abcdef"
    for len(s) < 1000000 { s = s + s }
    c, ok := wsc_open("127.0.0.1", ` + port + `, "/")
    n := wsc_send(c, s)
    back, good := wsc_recv(c)
    println(str(good) + " " + str(back == s))
}`)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "1 true" {
		t.Errorf("got %q", out)
	}
	if peak := peakRSSKB(t, pid); peak > 100*1024 {
		t.Errorf("server peak RSS %d KB for a 1 MiB message", peak)
	}
}
