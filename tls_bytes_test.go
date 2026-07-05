package main

import (
	"bytes"
	"crypto/tls"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestTLSReadWriteBytesRoundTrip: tls_read_bytes/tls_write_bytes are the
// binary-safe counterparts of tls_read/tls_write (already exercised by
// TestServerTLS). An MFL TLS server echoes back whatever bytes it reads,
// including an embedded NUL — proving the byte path doesn't truncate at NUL
// the way a C-string-based tls_read/tls_write would.
func TestTLSReadWriteBytesRoundTrip(t *testing.T) {
	const port = 47662
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")

	src := `func main() {
	ctx := tls_server_ctx(` + quoteGo(certPath) + `, ` + quoteGo(keyPath) + `)
	if ctx == 0 { println("ctx fail") exit(1) }
	srv := listen(` + itoa(port) + `)
	fd := accept(srv)
	tls := tls_accept(ctx, fd)
	if tls == 0 { println("handshake fail") exit(1) }
	b := tls_read_bytes(tls)
	tls_write_bytes(tls, b)
	tls_close(tls)
}`
	bin, err := os.CreateTemp("", "mfl-tls-bytes-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("tls-bytes server program failed to compile: %v", err)
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

	payload := []byte{0x48, 0x65, 0x00, 0x61, 0xff}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write to server: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read from server: %v", err)
	}
	if got := buf[:n]; !bytes.Equal(got, payload) {
		t.Fatalf("tls_read_bytes/tls_write_bytes round trip = %x, want %x (embedded NUL at index 2)", got, payload)
	}
}
