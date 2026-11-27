package main

import (
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// rawHTTPServer answers every loopback connection with reply(requestHead): the
// exact bytes to send, and whether to leave the connection open afterwards.
func rawHTTPServer(t *testing.T, reply func(head string) (resp string, hold bool)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback available: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(done); ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(20 * time.Second))
				var head []byte
				buf := make([]byte, 4096)
				for !strings.Contains(string(head), "\r\n\r\n") {
					n, err := c.Read(buf)
					head = append(head, buf[:n]...)
					if err != nil {
						return
					}
				}
				resp, hold := reply(string(head))
				c.Write([]byte(resp))
				if hold {
					<-done
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestHTTPClientResponses(t *testing.T) {
	long := "/long/" + strings.Repeat("a", 5000)
	var addr string
	addr = rawHTTPServer(t, func(head string) (string, bool) {
		lines := strings.Split(head, "\r\n")
		f := strings.Fields(lines[0])
		method, path := f[0], f[1]
		ok := func(body string) string {
			return fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
		}
		to := func(loc string) string {
			return "HTTP/1.1 302 Found\r\nLocation: " + loc + "\r\nContent-Length: 0\r\n\r\n"
		}
		switch path {
		case "/hang":
			return "", true
		case "/stall":
			return "HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nabc", true
		case "/stall-headers":
			return "HTTP/1.1 200 OK\r\n", true
		case "/held-open":
			return ok("whole"), true
		case "/short":
			return "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nonly this", false
		case "/cut-headers":
			return "HTTP/1.1 200 OK\r\nContent-Le", false
		case "/bad-length":
			return "HTTP/1.1 200 OK\r\nContent-Length: abc\r\n\r\nx", false
		case "/other-length":
			return "HTTP/1.1 200 OK\r\nX-Content-Length: 1\r\n\r\nclose-delimited", false
		case "/chunked":
			return "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\nA;x=1\r\n0123456789\r\n0\r\n\r\n", false
		case "/chunked-cut":
			return "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n", false
		case "/chunked-midchunk":
			return "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n9\r\nabc", false
		case "/nul":
			return "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nab\x00cd", false
		case "/empty":
			return "", false
		case "/garbage":
			return "SSH-2.0-nope\r\n\r\n", false
		case "/interim":
			return "HTTP/1.1 100 Continue\r\n\r\n" + ok("after"), false
		case "/head":
			return "HTTP/1.1 200 OK\r\nContent-Length: 50\r\n\r\n", false
		case "/none":
			return "HTTP/1.1 204 No Content\r\n\r\n", false
		case "/dir/rel":
			return to("landed"), false
		case "/dir/up":
			return to("../landed"), false
		case "/dir/abs-path":
			return to("/landed"), false
		case "/dir/query?old=1":
			return to("?new=2"), false
		case "/net-path":
			return to("//" + addr + "/landed"), false
		case "/other-scheme":
			return to("ftp://example.invalid/x"), false
		case "/?q=1":
			return ok("query kept"), false
		case "/a%20b":
			return ok("space encoded"), false
		case "/host":
			for _, l := range lines {
				if strings.HasPrefix(l, "Host: ") {
					return ok(l), false
				}
			}
		}
		if strings.HasPrefix(path, "/long/") {
			return ok(fmt.Sprint(len(path))), false
		}
		return ok(method + " " + path), false
	})
	base := "http://" + addr
	cases := []struct{ method, url, want string }{
		{"GET", base + "/held-open", "200|whole|"},
		{"GET", base + "/hang", "0||timeout"},
		{"GET", base + "/stall", "0||timeout"},
		{"GET", base + "/stall-headers", "0||timeout"},
		{"GET", base + "/short", "0||truncated"},
		{"GET", base + "/cut-headers", "0||truncated"},
		{"GET", base + "/bad-length", "0||response"},
		{"GET", base + "/other-length", "200|close-delimited|"},
		{"GET", base + "/chunked", "200|abc0123456789|"},
		{"GET", base + "/chunked-cut", "0||truncated"},
		{"GET", base + "/chunked-midchunk", "0||truncated"},
		{"GET", base + "/nul", "0||nul"},
		{"GET", base + "/empty", "0||empty"},
		{"GET", base + "/garbage", "0||response"},
		{"GET", base + "/interim", "200|after|"},
		{"HEAD", base + "/head", "200||"},
		{"GET", base + "/none", "204||"},
		{"GET", base + "/dir/rel", "200|GET /dir/landed|"},
		{"GET", base + "/dir/up", "200|GET /landed|"},
		{"GET", base + "/dir/abs-path", "200|GET /landed|"},
		{"HEAD", base + "/dir/abs-path", "200||"},
		{"GET", base + "/dir/query?old=1", "200|GET /dir/query?new=2|"},
		{"GET", base + "/net-path", "200|GET /landed|"},
		{"GET", base + "/other-scheme", "302||"},
		{"GET", base + "?q=1", "200|query kept|"},
		{"GET", base + "/frag#part", "200|GET /frag|"},
		{"GET", base + "/a b", "200|space encoded|"},
		{"GET", base + "/host", "200|Host: " + addr + "|"},
		{"GET", base + long, fmt.Sprintf("200|%d|", len(long))},
		{"GET", "HTTP://" + addr + "/caps", "200|GET /caps|"},
		{"GET", "ftp://" + addr + "/x", "0||scheme"},
		{"GET", "http://127.0.0.1:99999/", "0||url"},
		{"GET", "http://127.0.0.1:8x/", "0||url"},
		{"GET", "http:///nohost", "0||url"},
	}
	var src strings.Builder
	src.WriteString(`func show(m, u) {
    s, b, e := http_request(m, u, []string{}, "")
    println(str(s) + "|" + b + "|" + e)
}
func main() {
`)
	for _, c := range cases {
		fmt.Fprintf(&src, "    show(%q, %q)\n", c.method, c.url)
	}
	fmt.Fprintf(&src, "    println(\"[\" + https_get(%q) + \"]\")\n}\n", base+"/short")

	t.Setenv("MFL_HTTP_TIMEOUT_MS", "500")
	out, err := RunCaptured(progFromSrc(t, src.String()))
	if err != nil {
		if strings.Contains(err.Error(), "ssl") {
			t.Skipf("OpenSSL unavailable: %v", err)
		}
		t.Fatalf("run: %v", err)
	}
	got := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(got) != len(cases)+1 {
		t.Fatalf("want %d lines, got %d:\n%s", len(cases)+1, len(got), out)
	}
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s %.80s: got %.120q, want %.120q", c.method, c.url, got[i], c.want)
		}
	}
	if got[len(cases)] != "[]" {
		t.Errorf("https_get of a truncated body should be empty, got %q", got[len(cases)])
	}

	// MFL_HTTP_MAX_REDIRECTS=0 hands the 3xx back
	t.Setenv("MFL_HTTP_MAX_REDIRECTS", "0")
	out, err = RunCaptured(progFromSrc(t, fmt.Sprintf(`func main() {
    s, b, e := http_get(%q)
    println(str(s) + "|" + b + "|" + e)
}`, base+"/dir/rel")))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "302||" {
		t.Errorf("redirects disabled: got %q, want 302||", out)
	}
}

func TestBytesXorAndReadExact(t *testing.T) {
	addr := rawHTTPServer(t, func(string) (string, bool) { return "abcdefgh", false })
	port := addr[strings.LastIndex(addr, ":")+1:]
	out, err := RunCaptured(progFromSrc(t, `func main() {
    k := from_hex("0102")
    x := bytes_xor(bytes("hello"), k)
    println(to_hex(x))
    println(bytes_str(bytes_xor(x, k)))
    println(bytes_str(bytes_xor(bytes("same"), bytes(""))))
    c := dial("127.0.0.1", `+port+`)
    n := write(c, "go\r\n\r\n")
    println(bytes_str(read_bytes_exact(c, 3)))
    println(len(read_bytes_exact(c, 0)))
    println(bytes_str(read_bytes_exact(c, 4)))
    println(len(read_bytes_exact(c, 1000000000000)))
    close(c)
}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "69676d6e6e\nhello\nsame\nabc\n0\ndefg\n0\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// The same verdicts over TLS, where the end of a stream is reported differently.
func TestHTTPSClientResponses(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses SSL_CERT_FILE")
	}
	done := make(chan struct{})
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			io.WriteString(w, "secure")
		case "/chunked":
			io.WriteString(w, "a")
			w.(http.Flusher).Flush()
			io.WriteString(w, "b")
		case "/short":
			w.Header().Set("Content-Length", "100")
			io.WriteString(w, "only this")
		case "/stall":
			io.WriteString(w, "a")
			w.(http.Flusher).Flush()
			<-done
		}
	}))
	defer ts.Close()
	defer close(done)
	// accepts TCP and never answers the TLS handshake
	silent := rawHTTPServer(t, func(string) (string, bool) { return "", true })
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", ca)
	t.Setenv("MFL_HTTP_TIMEOUT_MS", "500")
	out, err := RunCaptured(progFromSrc(t, fmt.Sprintf(`func show(u) {
    s, b, e := http_get(u)
    println(str(s) + "|" + b + "|" + e)
}
func main() {
    show("%[1]s/ok")
    show("%[1]s/chunked")
    show("%[1]s/short")
    show("%[1]s/stall")
    show("https://%[2]s/")
}`, ts.URL, silent)))
	if err != nil {
		if strings.Contains(err.Error(), "ssl") {
			t.Skipf("OpenSSL unavailable: %v", err)
		}
		t.Fatalf("run: %v", err)
	}
	if want := "200|secure|\n200|ab|\n0||truncated\n0||timeout\n0||timeout\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
