package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// httpstreamProg composes framework/httpstream.src + the test app.
func httpstreamProg(t *testing.T, app string) *Program {
	t.Helper()
	src, err := os.ReadFile("framework/httpstream.src")
	if err != nil {
		t.Skip("framework/httpstream.src not found")
	}
	prog, perr := progFromSrcErr(string(src) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// hsReadHTTPHeaders reads a request through the blank line (headers only).
func hsReadHTTPHeaders(c net.Conn) (string, error) {
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
			return "", fmt.Errorf("request headers too large")
		}
	}
}

func hsReadN(c net.Conn, n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(c, b)
	return b, err
}

// hsCL extracts a Content-Length header value (0 if absent).
func hsCL(req string) int {
	for _, line := range strings.Split(req, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			v := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return 0
}

func hsRequestPath(req string) string {
	line := strings.SplitN(req, "\r\n", 2)[0]
	f := strings.Fields(line)
	if len(f) >= 2 {
		return f[1]
	}
	return ""
}

// hsHandleConn plays one scripted response per path:
//
//	/cl       -> Content-Length body, written in three segments
//	/chunked  -> chunked body with the chunk header AND payload split across writes
//	/echo     -> POST: replies with the request body (Content-Length framed)
//	/sse      -> text/event-stream, no length: events trickle in over ~1.2s
func hsHandleConn(c net.Conn) {
	defer c.Close()
	req, err := hsReadHTTPHeaders(c)
	if err != nil {
		return
	}
	cl := hsCL(req)
	var reqBody []byte
	if cl > 0 {
		reqBody, err = hsReadN(c, cl)
		if err != nil {
			return
		}
	}
	path := hsRequestPath(req)
	switch path {
	case "/cl":
		io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 23\r\nX-Stream: yes\r\n\r\n")
		io.WriteString(c, "Hello, ")
		time.Sleep(80 * time.Millisecond)
		io.WriteString(c, "streaming ")
		time.Sleep(80 * time.Millisecond)
		io.WriteString(c, "world!")
	case "/chunked":
		io.WriteString(c, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n")
		io.WriteString(c, "5\r\nhel") // chunk header complete, payload split
		time.Sleep(60 * time.Millisecond)
		io.WriteString(c, "lo\r\n") // completes "hello"
		io.WriteString(c, "6\r\n worl")
		time.Sleep(60 * time.Millisecond)
		io.WriteString(c, "d\r\n") // completes " world"
		io.WriteString(c, "0\r\nX-Trailer: t\r\n\r\n")
	case "/echo":
		io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: "+strconv.Itoa(len(reqBody))+"\r\n\r\n")
		c.Write(reqBody)
	case "/sse":
		io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\n\r\n")
		io.WriteString(c, ": connected\n\n") // comment-only block: no dispatch
		io.WriteString(c, "data: first\n\n")
		time.Sleep(400 * time.Millisecond)
		io.WriteString(c, "event: token\ndata: second\n\n")
		time.Sleep(400 * time.Millisecond)
		io.WriteString(c, "data: x\ndata: y\n\n") // multi-line data -> "x\ny"
		time.Sleep(400 * time.Millisecond)
		io.WriteString(c, "data: third\n\n")
		time.Sleep(100 * time.Millisecond)
	}
}

// TestHttpStreamRoundTrip exercises the pure-MFL streaming client against a
// scripted loopback server: Content-Length reassembly, chunked decoding across
// fragmented writes, a POST echo, SSE event delivery, and the refusal cases.
func TestHttpStreamRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback available: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go hsHandleConn(c)
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	app := fmt.Sprintf(`
func drain(h) (out) {
    out = ""
    while 1 == 1 {
        c, more := hs_read(h)
        if more == 0 { return }
        out = out + c
    }
}
func main() {
    base := "http://127.0.0.1:%d"

    h, ok := hs_open("GET", base + "/cl", []string{}, "")
    println("CL ok=" + str(ok) + " st=" + str(hs_status(h)) + " x=" + hs_header(h, "X-Stream"))
    println("CL body=<" + drain(h) + ">")
    hs_close(h)

    h2, ok2 := hs_open("GET", base + "/chunked", []string{}, "")
    println("CHK ok=" + str(ok2) + " st=" + str(hs_status(h2)))
    println("CHK body=<" + drain(h2) + ">")
    hs_close(h2)

    h3, ok3 := hs_open("POST", base + "/echo", []string{"X-Test: abc"}, "ping-body")
    println("ECHO ok=" + str(ok3) + " body=<" + drain(h3) + ">")
    hs_close(h3)

    h4, ok4 := hs_open("GET", base + "/sse", []string{}, "")
    println("SSE ok=" + str(ok4) + " ct=" + hs_header(h4, "Content-Type"))
    t0 := now_ms()
    while 1 == 1 {
        ev, dt, eok := hs_sse_next(h4)
        if eok == 0 { break }
        println("SSE t=" + str(now_ms() - t0) + " ev=<" + ev + "> data=<" + dt + ">")
    }
    hs_close(h4)

    h5, ok5 := hs_open("GET", "ftp://127.0.0.1:%d/x", []string{}, "")
    println("BAD ok=" + str(ok5))
    h6, ok6 := hs_open("GET", base + "/cl", []string{"X: a\r\nEvil: 1"}, "")
    println("INJ ok=" + str(ok6))
}`, port, port)

	out, err := RunCaptured(httpstreamProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Content-Length: segmented body reassembles; status + custom header parsed.
	if !strings.Contains(out, "CL ok=1 st=200 x=yes") {
		t.Fatalf("CL open should succeed with status 200 and X-Stream header; got:\n%s", out)
	}
	if !strings.Contains(out, "CL body=<Hello, streaming world!>") {
		t.Fatalf("CL body mismatch; got:\n%s", out)
	}
	// Chunked: payload and chunk headers split across TCP writes still decode.
	if !strings.Contains(out, "CHK ok=1 st=200") || !strings.Contains(out, "CHK body=<hello world>") {
		t.Fatalf("chunked decode mismatch; got:\n%s", out)
	}
	// POST: request head + body reached the server; echo came back framed.
	if !strings.Contains(out, "ECHO ok=1 body=<ping-body>") {
		t.Fatalf("echo mismatch; got:\n%s", out)
	}
	// SSE: comment skipped, event name honored, multi-line data joined.
	for _, want := range []string{
		"SSE ok=1 ct=text/event-stream",
		"ev=<message> data=<first>",
		"ev=<token> data=<second>",
		"data=<x\ny>",
		"ev=<message> data=<third>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing SSE expectation %q; got:\n%s", want, out)
		}
	}
	// Incrementality: events must arrive as the server writes them (~400ms
	// apart), not buffered until close. If hs_sse_next waited for EOF, all
	// timestamps would cluster at the end and the gaps would collapse.
	var sseTimes []int
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "SSE t=") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(line, "SSE t="))
		if len(f) > 0 {
			if n, err := strconv.Atoi(strings.SplitN(f[0], " ", 2)[0]); err == nil {
				sseTimes = append(sseTimes, n)
			}
		}
	}
	if len(sseTimes) != 4 {
		t.Fatalf("expected 4 SSE events, got times %v; output:\n%s", sseTimes, out)
	}
	for i := 1; i < len(sseTimes); i++ {
		if gap := sseTimes[i] - sseTimes[i-1]; gap < 150 {
			t.Fatalf("SSE event %d arrived %dms after the previous — not streaming; times %v", i, gap, sseTimes)
		}
	}
	// Refusals: non-http(s) scheme and CR/LF in a header line both fail closed.
	if !strings.Contains(out, "BAD ok=0") {
		t.Fatalf("non-http scheme should be refused; got:\n%s", out)
	}
	if !strings.Contains(out, "INJ ok=0") {
		t.Fatalf("CR/LF header line should be refused; got:\n%s", out)
	}
}
