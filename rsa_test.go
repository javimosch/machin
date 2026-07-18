package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// rsa_generate + rsa_sign_pkcs1_sha256 + rsa_verify_pkcs1_sha256 form a
// self-contained round trip in pure MFL: mint a keypair, sign a message, verify
// it holds, and confirm a tampered message fails. This is the RS256 primitive
// that issue #484 asks for (unblocks local JWT signing/verification + SAML SP
// keys) without any PEM/X.509 handling on the MFL side.
func TestRSASignVerifyRoundTrip(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    priv, pub := rsa_generate(2048)
    println("privpem=" + str(len(priv) > 0))
    println("pubpem=" + str(len(pub) > 0))

    msg := bytes("the quick brown fox jumps over the lazy dog")
    sig := rsa_sign_pkcs1_sha256(priv, msg)
    println("siglen=" + str(len(sig) == 256))

    ok := rsa_verify_pkcs1_sha256(pub, msg, sig)
    println("verify=" + str(ok))

    bad := rsa_verify_pkcs1_sha256(pub, bytes("tampered payload"), sig)
    println("tampered=" + str(bad))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"privpem=true", "pubpem=true", "siglen=true",
		"verify=true", "tampered=false",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// rsa_verify_jwk_sha256 is THE RS256 JWT path: verify a signature straight from
// a JWKS's raw modulus (n) and exponent (e) — no PEM, no X.509. We build a real
// RS256 vector in Go (the same crypto/rsa an IdP uses), feed n/e/msg/sig into an
// MFL program as hex, and assert the MFL builtin agrees: valid sig -> true, a
// flipped signature byte -> false. This mirrors verifying an IdP id_token
// against its published JWKS.
func TestRSAVerifyJWK(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	msg := []byte("id_token.signing.input.example")
	digest := sha256.Sum256(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// JWKS exposes n and e as base64url of the big-endian bytes; we pass the raw
	// bytes (hex here) — the builtin base64url-decodes on the MFL caller's side.
	nBytes := key.N.Bytes()
	eBytes := key.PublicKey.E // int; encode big-endian, trimming leading zeros
	eb := []byte{byte(eBytes >> 16), byte(eBytes >> 8), byte(eBytes)}
	for len(eb) > 1 && eb[0] == 0 {
		eb = eb[1:]
	}

	badSig := make([]byte, len(sig))
	copy(badSig, sig)
	badSig[0] ^= 0xff

	src := fmt.Sprintf(`
func main() {
    n := from_hex("%s")
    e := from_hex("%s")
    msg := from_hex("%s")
    sig := from_hex("%s")
    bad := from_hex("%s")
    println("valid=" + str(rsa_verify_jwk_sha256(n, e, msg, sig)))
    println("invalid=" + str(rsa_verify_jwk_sha256(n, e, msg, bad)))
}`,
		hex.EncodeToString(nBytes),
		hex.EncodeToString(eb),
		hex.EncodeToString(msg),
		hex.EncodeToString(sig),
		hex.EncodeToString(badSig),
	)

	prog := progFromSrc(t, src)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"valid=true", "invalid=false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// Cross-key negative: a signature made by one key must NOT verify against a
// different key's public PEM. Guards against a builtin that ignores the key.
func TestRSAWrongKeyFails(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    privA, _ := rsa_generate(2048)
    _, pubB := rsa_generate(2048)
    msg := bytes("cross key check")
    sig := rsa_sign_pkcs1_sha256(privA, msg)
    println("wrongkey=" + str(rsa_verify_pkcs1_sha256(pubB, msg, sig)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "wrongkey=false") {
		t.Fatalf("expected wrongkey=false in:\n%s", out)
	}
}
