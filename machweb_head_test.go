package main

// Regression tests for #746: nothing an application passes into a response head
// or an event field may add a line to it.

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const headRefusal = "HTTP/1.1 500 Internal Server Error\r\nContent-Type: text/plain\r\nContent-Length: 21\r\nConnection: close\r\n\r\ninternal server error"

const headApp = `
func handle(req) (res) {
    which := query(req, "c")
    v := query(req, "v")
    res = set_cookie(ok_text("body"), "keep", "yes")
    if which == "redirect" { res = set_cookie(redirect(v), "keep", "yes") }
    if which == "cname" { res = set_cookie(res, v, "1") }
    if which == "cvalue" { res = set_cookie(res, "n", v) }
    if which == "clear" { res = clear_cookie(res, v) }
    if which == "hname" { res = with_header(res, v, "1") }
    if which == "hvalue" { res = with_header(res, "X-A", v) }
    if which == "ctype" { res = response("200 OK", v, "body") }
    if which == "status" { res = response(v, "text/plain", "body") }
    if which == "dlocation" { res.location = v }
    if which == "dcookie" { res.cookies = append(res.cookies, v) }
    if which == "dextra" { res.extra = append(res.extra, v) }
    if which == "bin" { res = with_header(ok_bytes(v, bytes("body")), "X-A", "1") }
    if which == "sctype" { res = stream_response("200 OK", v, func(conn) { w := write(2, "STREAM RAN\n") }) }
    if which == "sstatus" { res = stream_response(v, "text/plain", func(conn) { w := write(2, "STREAM RAN\n") }) }
    if which == "scookie" { res = set_cookie(sse(func(conn) { w := write(2, "STREAM RAN\n") }), "n", v) }
    if which == "sticky" { res = set_cookie(set_cookie(res, "n", v), "m", "fine") }
    if which == "lines" { res = ok_text("[" + cookie_lines(with_header(res, "X-A", v)) + "]") }
    if which == "sse" {
        res = sse(func(conn) {
            a := sse_data(conn, v)
            b := sse_comment(conn, v)
            c := sse_event(conn, "tick", v)
            d := sse_event(conn, v, "x")
            w := write(conn, "result " + str(d) + "\n")
        })
    }
}
`

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) take() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.b.String()
	s.b.Reset()
	return out
}

func headExchange(t *testing.T, c net.Conn, which, v string) string {
	t.Helper()
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	var enc strings.Builder
	for _, b := range []byte(v) {
		fmt.Fprintf(&enc, "%%%02X", b)
	}
	fmt.Fprintf(c, "GET /?c=%s&v=%s HTTP/1.1\r\nHost: x\r\n\r\n", which, enc.String())
	out, _ := io.ReadAll(c)
	return string(out)
}

func TestMachwebRefusesResponseHeadBreaks(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := "plain"
		if secure {
			name = "tls"
		}
		t.Run(name, func(t *testing.T) {
			port := freeLoopbackPort(t)
			dir := t.TempDir()
			main := fmt.Sprintf("\nfunc main() { serve_on(\"127.0.0.1\", %d, func(req) { return handle(req) }) }\n", port)
			if secure {
				cert, key := genSelfSignedCert(t, dir, "localhost")
				main = fmt.Sprintf("\nfunc main() { serve_tls(%d, %q, %q, func(req) { return handle(req) }) }\n", port, cert, key)
			}
			data, err := os.ReadFile("framework/machweb.src")
			if err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(dir, "server")
			if err := BuildBinary(progFromSrc(t, string(data)+headApp+main), bin, false); err != nil {
				t.Fatalf("build: %v", err)
			}
			var stderr syncBuf
			cmd := exec.Command(bin)
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
			addr := fmt.Sprintf("127.0.0.1:%d", port)
			dial := func() net.Conn {
				var c net.Conn
				var err error
				for i := 0; i < 200; i++ {
					if secure {
						c, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
					} else {
						c, err = net.Dial("tcp", addr)
					}
					if err == nil {
						return c
					}
					time.Sleep(20 * time.Millisecond)
				}
				t.Fatalf("dial: %v", err)
				return nil
			}
			get := func(which, v string) string { return headExchange(t, dial(), which, v) }
			refused := func(which, v, field string) {
				t.Helper()
				stderr.take()
				got := get(which, v)
				want := headRefusal
				if secure && strings.HasPrefix(which, "s") && which != "status" && which != "sticky" {
					if !strings.HasPrefix(got, "HTTP/1.1 501 ") {
						t.Fatalf("%s %q over TLS: %q", which, v, got)
					}
					return
				}
				if got != want {
					t.Fatalf("%s %q: got %q", which, v, got)
				}
				log := ""
				for i := 0; i < 100 && !strings.Contains(log, "\n"); i++ {
					time.Sleep(10 * time.Millisecond)
					log += stderr.take()
				}
				time.Sleep(20 * time.Millisecond)
				log += stderr.take()
				if log != "machweb: response refused: "+field+"\n" {
					t.Fatalf("%s %q: stderr %q", which, v, log)
				}
			}

			breaks := []string{"a\r\nX-Evil: 1", "a\rX-Evil: 1", "a\nX-Evil: 1", "a\r\n\r\n<b>", "\r", "\n"}
			for _, v := range breaks {
				refused("redirect", v, "location")
				refused("dlocation", v, "location")
				refused("cname", v, "set_cookie name")
				refused("cvalue", v, "set_cookie value")
				refused("clear", v, "clear_cookie name")
				refused("hname", v, "with_header name")
				refused("hvalue", v, "with_header value")
				refused("ctype", v, "content type")
				refused("bin", v, "content type")
				refused("status", v, "status")
				refused("dcookie", v, "cookie")
				refused("dextra", v, "header")
				refused("sctype", v, "content type")
				refused("sstatus", v, "status")
				refused("scookie", v, "set_cookie value")
				refused("sticky", v, "set_cookie value")
			}
			refused("cname", "a;b", "set_cookie name")
			refused("cname", "a=b", "set_cookie name")
			refused("cvalue", "a; Domain=example.org", "set_cookie value")
			refused("clear", "a;b", "clear_cookie name")
			refused("clear", "a=b", "clear_cookie name")
			refused("hname", "Location: elsewhere", "with_header name")
			refused("dextra", "", "header")

			// what stays allowed
			for _, ok := range [][3]string{
				{"redirect", "/a?b=c;d#e", "Location: /a?b=c;d#e\r\n"},
				{"cvalue", "a=b,c d", "Set-Cookie: n=a=b,c d; Path=/"},
				{"hvalue", "attachment; filename=\"a:b\"", "X-A: attachment; filename=\"a:b\"\r\n"},
				{"clear", "sid", "Set-Cookie: sid=; Path=/; Max-Age=0\r\n"},
			} {
				if got := get(ok[0], ok[1]); !strings.Contains(got, ok[2]) || !strings.Contains(got, "Set-Cookie: keep=yes") {
					t.Fatalf("%s: %q", ok[0], got)
				}
			}
			if got := get("lines", "a\nb"); !strings.HasSuffix(got, "\r\n\r\n[]") {
				t.Fatalf("cookie_lines of a refused response: %q", got)
			}
			if !secure {
				stderr.take()
				_, body, _ := strings.Cut(get("sse", "a\r\nb\rc\nd"), "\r\n\r\n")
				data := "data: a\ndata: b\ndata: c\ndata: d\n"
				want := data + "\n: a\n: b\n: c\n: d\n\nevent: tick\n" + data + "\nresult -1\n"
				if body != want {
					t.Fatalf("sse: got %q, want %q", body, want)
				}
				_, body, _ = strings.Cut(get("sse", ""), "\r\n\r\n")
				if want := "data: \n\n: \n\nevent: tick\ndata: \n\nevent: \ndata: x\n\n"; !strings.HasPrefix(body, want) {
					t.Fatalf("empty sse: %q", body)
				}
				time.Sleep(50 * time.Millisecond)
				if log := stderr.take(); strings.Contains(log, "STREAM RAN") {
					t.Fatalf("a refused stream ran: %q", log)
				}
			}
		})
	}
}
