package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"os"
	"strings"
	"testing"
)

// TestX509PubkeyVerify exercises the X.509 arm of pure-MFL SAML SSO (issue #484):
// the x509_pubkey builtin (extract an RSA n/e from a DER cert) and the
// sso_x509_rsa_verify helper (framework/sso.src). We mint a genuine self-signed
// RSA cert + an RSA-SHA256 signature in Go — exactly what a SAML IdP's
// <ds:X509Certificate> / <ds:SignatureValue> carry — and assert the MFL path:
// (1) verifies a valid signature against the cert's embedded key, and
// (2) rejects a tampered message.
func TestX509PubkeyVerify(t *testing.T) {
	sso, err := os.ReadFile("framework/sso.src")
	if err != nil {
		t.Skip("framework/sso.src not found")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	// A self-signed cert, like an IdP signing cert published in SAML metadata.
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "idp.example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	certB64 := base64.StdEncoding.EncodeToString(der) // <ds:X509Certificate> body

	msg := "the-canonicalized-assertion-bytes"
	digest := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigB64 := base64.StdEncoding.EncodeToString(sig) // <ds:SignatureValue> body

	app := "\n" +
		"func main() {\n" +
		"    cert := \"" + certB64 + "\"\n" +
		"    sig := \"" + sigB64 + "\"\n" +
		"    n, e := x509_pubkey(base64_decode_bytes(cert))\n" +
		"    println(\"nlen=\" + str(len(n)) + \" elen=\" + str(len(e)))\n" +
		"    good := sso_x509_rsa_verify(cert, bytes(\"" + msg + "\"), sig)\n" +
		"    println(\"valid=\" + str(good))\n" +
		"    bad := sso_x509_rsa_verify(cert, bytes(\"" + msg + "X\"), sig)\n" +
		"    println(\"tampered=\" + str(bad))\n" +
		"}\n"

	prog, perr := progFromSrcErr(string(sso) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"nlen=256 elen=3", // 2048-bit modulus + 65537 exponent, extracted from the DER cert
		"valid=1",         // genuine RSA-SHA256 signature verifies against the cert key
		"tampered=0",      // altered message -> signature fails
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// A non-RSA (here EC) certificate must yield an empty n/e — so a caller can
// detect "not an RSA cert" (and sso_x509_rsa_verify returns ok=0) rather than
// crash. Locks in the documented contract of x509_pubkey.
func TestX509PubkeyNonRSAEmpty(t *testing.T) {
	sso, err := os.ReadFile("framework/sso.src")
	if err != nil {
		t.Skip("framework/sso.src not found")
	}
	eckey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("eckey: %v", err)
	}
	tmpl := x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "ec.example.com"}}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &eckey.PublicKey, eckey)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	certB64 := base64.StdEncoding.EncodeToString(der)

	app := "\n" +
		"func main() {\n" +
		"    n, e := x509_pubkey(base64_decode_bytes(\"" + certB64 + "\"))\n" +
		"    println(\"nlen=\" + str(len(n)) + \" elen=\" + str(len(e)))\n" +
		"    ok := sso_x509_rsa_verify(\"" + certB64 + "\", bytes(\"whatever\"), \"AAAA\")\n" +
		"    println(\"ok=\" + str(ok))\n" +
		"}\n"
	prog, perr := progFromSrcErr(string(sso) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"nlen=0 elen=0", "ok=0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
