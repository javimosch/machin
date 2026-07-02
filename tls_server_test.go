package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// genSelfSignedCert writes a throwaway self-signed cert+key (PEM) for cn/ip to
// dir, returning their paths. Used both as the server-side cert for TestServerTLS
// and as the deliberately-untrusted cert for TestSTARTTLS's negative case.
func genSelfSignedCert(t *testing.T, dir, cn string) (certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{cn},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	certOut.Close()
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyOut.Close()
	return certPath, keyPath
}

// TestServerTLS: an MFL program terminates TLS itself (tls_server_ctx + accept +
// tls_accept), no reverse proxy — issue #260's core case. A real Go TLS client
// (InsecureSkipVerify — this is the TEST HARNESS trusting a throwaway cert, not
// the code under test relaxing verification) drives it end-to-end: handshake,
// write a request, read the response.
func TestServerTLS(t *testing.T) {
	const port = 47660
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")

	src := `func main() {
	ctx := tls_server_ctx(` + quoteGo(certPath) + `, ` + quoteGo(keyPath) + `)
	if ctx == 0 { println("ctx fail") exit(1) }
	srv := listen(` + itoa(port) + `)
	fd := accept(srv)
	tls := tls_accept(ctx, fd)
	if tls == 0 { println("handshake fail") exit(1) }
	req := tls_read(tls)
	println(str(len(req)))
	tls_write(tls, "pong")
	tls_close(tls)
}`
	bin, err := os.CreateTemp("", "mfl-tls-srv-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("server-TLS program failed to compile: %v", err)
	}

	cmd := exec.Command(bin.Name())
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	var conn *tls.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 200 * time.Millisecond}, "tcp", "127.0.0.1:"+itoa(port),
			&tls.Config{InsecureSkipVerify: true})
		if dialErr == nil {
			conn = c
			break
		}
		err = dialErr
		time.Sleep(50 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("could not connect to the MFL TLS server: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write to server: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read from server: %v", err)
	}
	if got := string(buf[:n]); got != "pong" {
		t.Fatalf("round trip: got %q, want \"pong\"", got)
	}
}

// TestSTARTTLS: tls_client_fd upgrades an already-connected, plaintext fd to
// TLS in place. A local Go server accepts a plain connection, waits for a
// trigger line (the STARTTLS negotiation), then upgrades the SAME net.Conn to
// TLS with tls.Server — the exact STARTTLS shape (SMTP dial -> EHLO/STARTTLS in
// plaintext -> upgrade). Its cert is self-signed and untrusted, so
// tls_client_fd — which verifies, same as https_get — must REJECT it. This
// proves the upgrade path's verification is genuinely active (mirrors the
// negative-case discipline in TestStaticBuildBundlesCACerts); a positive case
// against a real trusted cert isn't automated here for the same reason
// https_get's isn't (see #283's PR description for that manual verification —
// tls_client_fd shares mfl_tls_dial_e's already-proven handshake code exactly).
func TestSTARTTLS(t *testing.T) {
	const port = 47661
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		conn.Read(buf) // the plaintext STARTTLS trigger line
		conn.Write([]byte("OK\n"))
		tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		tconn.Handshake() // expected to fail: the MFL client won't trust this cert
	}()

	src := `func main() {
	fd := dial("127.0.0.1", ` + itoa(port) + `)
	if fd < 0 { println("dial fail") exit(1) }
	write(fd, "STARTTLS\r\n")
	read(fd)
	tls := tls_client_fd(fd, "localhost")
	if tls == 0 { println("REJECTED") exit(0) }
	println("ACCEPTED (should not happen: untrusted cert)")
}`
	bin, err := os.CreateTemp("", "mfl-starttls-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("STARTTLS program failed to compile: %v", err)
	}

	out, err := exec.Command(bin.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("running the STARTTLS program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "REJECTED\n" {
		t.Fatalf("expected the untrusted STARTTLS cert to be rejected, got: %q", got)
	}
}

func quoteGo(s string) string { return `"` + s + `"` }

// TestServeTLS: framework/machweb.src's serve_tls (the actual issue #260
// deliverable — a router served over native TLS, no reverse proxy), not just the
// raw builtins TestServerTLS already covers. A real Go TLS client drives a full
// HTTP round trip through the router.
func TestServeTLS(t *testing.T) {
	data, err := os.ReadFile("framework/machweb.src")
	if err != nil {
		t.Skip("framework/machweb.src not found")
	}
	const port = 47662
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")

	app := `func main() {
	r := new_router()
	route(r, "GET", "/hi", func(req) { return ok_text("hello from serve_tls") })
	serve_tls(` + itoa(port) + `, ` + quoteGo(certPath) + `, ` + quoteGo(keyPath) + `, func(req) { return dispatch(r, req) })
}`
	prog, perr := progFromSrcErr(string(data) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}

	bin, err := os.CreateTemp("", "mfl-serve-tls-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("serve_tls app failed to compile: %v", err)
	}

	cmd := exec.Command(bin.Name())
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	var resp *http.Response
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, getErr := client.Get("https://127.0.0.1:" + itoa(port) + "/hi")
		if getErr == nil {
			resp = r
			break
		}
		err = getErr
		time.Sleep(50 * time.Millisecond)
	}
	if resp == nil {
		t.Fatalf("could not reach serve_tls: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "hello from serve_tls" {
		t.Fatalf("got status=%d body=%q, want 200 \"hello from serve_tls\"", resp.StatusCode, body)
	}
}
