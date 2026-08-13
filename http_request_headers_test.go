package main

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// http_request header validation — #627.
//
// The header lines are joined into the request head verbatim, so a value the
// caller interpolated ("Authorization: Bearer " + token) that contains CR or LF
// forges additional headers, and with a doubled CRLF smuggles a whole second
// request. That is request splitting whenever the token comes from anywhere the
// program does not fully control — an env var, a config file, a prior response.
//
// The fix REFUSES rather than sanitizes, and reports it: (0, "", "header").
// Silently dropping the header would leave the caller believing it was sent.
//
// Ports are deliberately BELOW the ephemeral range (typically 32768-60999): a
// port inside it can be taken as the SOURCE port of an unrelated outbound
// connection, and then the bind fails for reasons unrelated to the test.
const (
	httpHdrPort    = 17666
	httpInjectPort = 17667
)

// httpHeadEcho accepts ONE plain HTTP connection, reads the request head, and
// answers 200 with a fixed body. The head is delivered on the returned channel.
func httpHeadEcho(t *testing.T, port int) <-chan string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			got <- "accept error: " + err.Error()
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		var head strings.Builder
		br := bufio.NewReader(conn)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				got <- "read error: " + err.Error()
				return
			}
			head.WriteString(line)
			if line == "\r\n" {
				break
			}
		}
		got <- head.String()
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok"))
		time.Sleep(100 * time.Millisecond)
	}()
	return got
}

func runMFL(t *testing.T, src string) string {
	t.Helper()
	bin, err := os.CreateTemp("", "mfl-httpreq-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	t.Cleanup(func() { os.Remove(bin.Name()) })
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("compile: %v", err)
	}
	out, err := exec.Command(bin.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	return string(out)
}

// A CRLF-bearing header must be refused with err "header", and NOTHING may be
// sent — the server must see no connection at all.
func TestHTTPRequestRefusesCRLFInHeaders(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a server and compiles a binary")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(httpInjectPort))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepted int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&accepted, 1)
			c.Close()
		}
	}()

	// The classic split: a token that closes the header block and starts a
	// second request the server would treat as genuine.
	src := `func main() {
	url := "http://127.0.0.1:` + itoa(httpInjectPort) + `/a"
	st, body, err := http_request("GET", url, []string{"X-Token: abc\r\nX-Injected: yes"}, "")
	println("st=" + str(st) + " body=[" + body + "] err=" + err)
	st2, _, err2 := http_request("GET", url, []string{"X-A: 1\r\n\r\nGET /admin HTTP/1.1\r\nHost: x"}, "")
	println("st2=" + str(st2) + " err2=" + err2)
	st3, _, err3 := http_request("GET", url, []string{"X-B: ok\nX-Bare-LF: yes"}, "")
	println("st3=" + str(st3) + " err3=" + err3)
}`
	out := runMFL(t, src)
	for _, want := range []string{
		"st=0 body=[] err=header",
		"st2=0 err2=header",
		"st3=0 err3=header", // a bare LF splits headers too, not just CRLF
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if n := atomic.LoadInt32(&accepted); n != 0 {
		t.Fatalf("refusal must happen before dialing, but the server accepted %d connection(s)", n)
	}
}

// Ordinary headers still reach the server verbatim, and an empty line is skipped
// rather than emitting a bare CRLF that would end the header block early.
func TestHTTPRequestSendsValidHeaders(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a server and compiles a binary")
	}
	reqCh := httpHeadEcho(t, httpHdrPort)
	src := `func main() {
	st, body, err := http_request("GET", "http://127.0.0.1:` + itoa(httpHdrPort) + `/a", []string{"Authorization: Bearer s3cr3t", "", "X-Trace: abc"}, "")
	println("st=" + str(st) + " body=[" + body + "] err=[" + err + "]")
}`
	out := runMFL(t, src)
	if !strings.Contains(out, "st=200 body=[ok] err=[]") {
		t.Fatalf("valid headers must still work, got:\n%s", out)
	}
	select {
	case req := <-reqCh:
		for _, want := range []string{"Authorization: Bearer s3cr3t\r\n", "X-Trace: abc\r\n"} {
			if !strings.Contains(req, want) {
				t.Fatalf("request missing %q:\n%s", want, req)
			}
		}
		// The empty entry must not have terminated the head early: the request
		// head is exactly one block, and X-Trace (which follows it) survived.
		if strings.Count(req, "\r\n\r\n") != 1 {
			t.Fatalf("request head is not exactly one block:\n%s", req)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server never received the request")
	}
}
