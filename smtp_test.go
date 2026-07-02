package main

import (
	"crypto/tls"
	"net"
	"os"
	"strings"
	"testing"
)

func smtpProg(t *testing.T, app string) *Program {
	t.Helper()
	smtp, err := os.ReadFile("framework/smtp.src")
	if err != nil {
		t.Skip("framework/smtp.src not found")
	}
	prog, perr := progFromSrcErr(string(smtp) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// A full SMTP round-trip over the socket: a server goroutine plays the receiving side
// (smtp_recv) and the client (smtp_send) sends a message through the real 220/EHLO/AUTH/
// MAIL/RCPT/DATA/QUIT conversation. Proves the pure-MFL client and server interoperate,
// including AUTH LOGIN and multi-recipient + dot-stuffing.
func TestSMTPRoundTrip(t *testing.T) {
	app := `
func catch(srv) {
    conn := accept(srv)
    mail, ok := smtp_recv(conn)
    close(conn)
    if ok == 1 {
        println("CAUGHT from=" + mail.mail_from + " to=[" + mail.rcpt_to + "] subj=" + mail_header(mail.data, "Subject"))
        println("BODY=" + mail_body(mail.data))
    } else {
        println("CAUGHT-FAILED")
    }
}
func main() {
    port := 48261
    srv := listen(port)
    if srv < 0 { println("listen-failed")  return }
    go catch(srv)
    sleep(50)
    ok, errmsg := smtp_send("127.0.0.1", port, "alice@x", "b@y, c@z", "Hello", "line one\n.dotline\nline three", "user", "pass", 0)
    println("SENT ok=" + str(ok) + " err=[" + errmsg + "]")
    sleep(60)
}`
	out, err := RunCaptured(smtpProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "listen-failed") {
		t.Fatalf("loopback setup failed:\n%s", out)
	}
	if !strings.Contains(out, "SENT ok=1 err=[]") {
		t.Fatalf("smtp_send (with AUTH) should succeed; got:\n%s", out)
	}
	// the server caught it with both recipients and the subject parsed from the headers
	if !strings.Contains(out, "CAUGHT from=alice@x to=[b@y, c@z] subj=Hello") {
		t.Fatalf("server should catch the message with both recipients + subject; got:\n%s", out)
	}
	// dot-stuffing round-tripped: the body line ".dotline" survives intact (not "" / "dotline")
	if !strings.Contains(out, ".dotline") {
		t.Fatalf("a dot-prefixed body line should survive dot-stuffing; got:\n%s", out)
	}
}

// smtp_send's STARTTLS path (use_tls=1): dial plaintext, EHLO, STARTTLS,
// tls_client_fd to upgrade, re-EHLO — driven against a real Go SMTP-shaped
// server that speaks the plaintext dance then upgrades with tls.Server,
// exactly like a real relay (Gmail/SendGrid/SES on :587). Its cert is
// self-signed and untrusted, so tls_client_fd — which verifies, same as
// https_get — must REJECT it: proves the upgrade path's verification is
// genuinely active, the same discipline as TestSTARTTLS in
// tls_server_test.go. The positive case (a real successful STARTTLS
// negotiation all the way to the auth-gated MAIL FROM step) was verified
// manually against smtp.gmail.com:587 with machin-mail — see issue #260.
func TestSMTPSendStartTLS(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := genSelfSignedCert(t, dir, "localhost")
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}

	const port = 48262
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
		buf := make([]byte, 4096)
		conn.Write([]byte("220 test.local ESMTP\r\n"))
		conn.Read(buf) // EHLO
		conn.Write([]byte("250-test.local\r\n250 STARTTLS\r\n"))
		conn.Read(buf) // STARTTLS
		conn.Write([]byte("220 2.0.0 Ready to start TLS\r\n"))
		tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		tconn.Handshake() // expected to fail: the MFL client won't trust this cert
	}()

	app := `func main() {
	ok, errmsg := smtp_send("127.0.0.1", ` + itoa(port) + `, "a@x", "b@y", "Hi", "body", "", "", 1)
	println("SENT ok=" + str(ok) + " err=[" + errmsg + "]")
}`
	out, err := RunCaptured(smtpProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "SENT ok=0 err=[TLS handshake failed]") {
		t.Fatalf("expected the untrusted STARTTLS cert to be rejected, got:\n%s", out)
	}
}
