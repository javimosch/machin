package main

import (
	"bufio"
	"crypto/tls"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// wss_open(url, headers) — #622. The WebSocket upgrade is an HTTP/1.1 request,
// so a server can (and the remotecmd relay does) enforce auth on it. Without
// this the handshake carried no caller-supplied headers and such a server was
// simply unreachable from MFL.
//
// These tests drive a REAL TLS WebSocket handshake rather than asserting on
// emitted C: the whole claim is "the header reaches the wire", and only running
// it can show that. The client verifies certs (SSL_VERIFY_PEER + hostname), so
// the throwaway cert is fed to it through SSL_CERT_FILE, which OpenSSL honours
// via SSL_CTX_set_default_verify_paths.

// Ports are deliberately BELOW the ephemeral range (typically 32768-60999): a
// port inside it can be taken as the SOURCE port of an unrelated outbound
// connection, after which the bind fails for reasons unrelated to the test.
const (
	wssHdrPort    = 17664
	wssInjectPort = 17665
)

// wssUpgradeEcho accepts ONE TLS connection, reads the upgrade request, answers
// 101, then sends a single unmasked text frame echoing the Authorization header
// it saw (or "none"). The full request head is delivered on the returned channel.
func wssUpgradeEcho(t *testing.T, port int, certPath, keyPath string) <-chan string {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:"+itoa(port), &tls.Config{Certificates: []tls.Certificate{cert}})
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

		auth := "none"
		for _, l := range strings.Split(head.String(), "\r\n") {
			if strings.HasPrefix(strings.ToLower(l), "authorization:") {
				auth = strings.TrimSpace(l[len("authorization:"):])
			}
		}
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
		// one server->client text frame, unmasked (servers must not mask)
		conn.Write(append([]byte{0x81, byte(len(auth))}, auth...))
		time.Sleep(200 * time.Millisecond) // let the client read before FIN
	}()
	return got
}

// runWSSClient compiles and runs an MFL program, trusting certPath.
func runWSSClient(t *testing.T, src, certPath string) string {
	t.Helper()
	bin, err := os.CreateTemp("", "mfl-wss-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	t.Cleanup(func() { os.Remove(bin.Name()) })
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("wss client failed to compile: %v", err)
	}
	cmd := exec.Command(bin.Name())
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+certPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	return string(out)
}

// The header must arrive at the server verbatim, and the two-arg call must
// otherwise behave exactly like the one-arg one.
func TestWSSOpenSendsCustomHeaders(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a TLS server and compiles a binary")
	}
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")
	reqCh := wssUpgradeEcho(t, wssHdrPort, certPath, keyPath)

	src := `func main() {
	h := wss_open("wss://localhost:` + itoa(wssHdrPort) + `/chat", []string{"Authorization: Bearer s3cr3t", "X-Trace: abc123"})
	if h == 0 { println("open fail") exit(1) }
	println("echo=" + wss_recv(h))
	wss_close(h)
}`
	out := runWSSClient(t, src, certPath)
	if !strings.Contains(out, "echo=Bearer s3cr3t") {
		t.Fatalf("server did not see the Authorization header; client said:\n%s", out)
	}

	select {
	case req := <-reqCh:
		// Both headers, and the handshake machin already sent, must survive.
		for _, want := range []string{
			"Authorization: Bearer s3cr3t\r\n",
			"X-Trace: abc123\r\n",
			"Sec-WebSocket-Key: ",
			"Sec-WebSocket-Version: 13\r\n",
			"Upgrade: websocket\r\n",
			"Host: localhost\r\n",
		} {
			if !strings.Contains(req, want) {
				t.Fatalf("upgrade request missing %q:\n%s", want, req)
			}
		}
		if strings.Count(req, "\r\n\r\n") != 1 {
			t.Fatalf("request head is not exactly one block:\n%s", req)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server never received the upgrade request")
	}
}

// A header line holding CR or LF would let a token forge extra headers or a
// second request. wss_open must REFUSE the call rather than sanitize it — and
// must not open a connection at all, so the server sees nothing.
func TestWSSOpenRefusesCRLFInHeaders(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a TLS server and compiles a binary")
	}
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:"+itoa(wssInjectPort), &tls.Config{Certificates: []tls.Certificate{cert}})
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

	src := `func main() {
	h := wss_open("wss://localhost:` + itoa(wssInjectPort) + `/chat", []string{"X-A: 1\r\nX-Injected: 2"})
	println("handle=" + str(h))
}`
	out := runWSSClient(t, src, certPath)
	if !strings.Contains(out, "handle=0") {
		t.Fatalf("a CRLF-bearing header must be refused with handle 0, got:\n%s", out)
	}
	time.Sleep(300 * time.Millisecond)
	if n := atomic.LoadInt32(&accepted); n != 0 {
		t.Fatalf("refusal must happen before dialing, but the server accepted %d connection(s)", n)
	}
}

// The one-arg form is untouched: same handshake, no extra headers.
func TestWSSOpenWithoutHeadersUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a TLS server and compiles a binary")
	}
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")
	port := wssHdrPort + 100
	reqCh := wssUpgradeEcho(t, port, certPath, keyPath)

	src := `func main() {
	h := wss_open("wss://localhost:` + itoa(port) + `/chat")
	if h == 0 { println("open fail") exit(1) }
	println("echo=" + wss_recv(h))
	wss_close(h)
}`
	out := runWSSClient(t, src, certPath)
	if !strings.Contains(out, "echo=none") {
		t.Fatalf("one-arg wss_open must send no Authorization header, client said:\n%s", out)
	}
	select {
	case req := <-reqCh:
		if strings.Contains(strings.ToLower(req), "authorization:") {
			t.Fatalf("one-arg wss_open leaked an Authorization header:\n%s", req)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server never received the upgrade request")
	}
}

// Long headers must not silently truncate the handshake: the request buffer was
// a fixed 4 kB before caller-supplied lines could reach it.
func TestWSSOpenLongHeadersDoNotTruncate(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a TLS server and compiles a binary")
	}
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")
	port := wssHdrPort + 200
	reqCh := wssUpgradeEcho(t, port, certPath, keyPath)

	big := strings.Repeat("x", 6000)
	src := `func main() {
	h := wss_open("wss://localhost:` + itoa(port) + `/chat", []string{"Authorization: Bearer ` + big + `"})
	if h == 0 { println("open fail") exit(1) }
	wss_close(h)
	println("ok")
}`
	out := runWSSClient(t, src, certPath)
	if !strings.Contains(out, "ok") {
		t.Fatalf("long header failed the handshake:\n%s", out)
	}
	select {
	case req := <-reqCh:
		if !strings.Contains(req, "Bearer "+big+"\r\n") {
			t.Fatalf("long header was truncated; request was %d bytes", len(req))
		}
		if !strings.HasSuffix(req, "\r\n\r\n") {
			t.Fatalf("request head did not terminate properly:\n%.200s", req)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server never received the upgrade request")
	}
}
