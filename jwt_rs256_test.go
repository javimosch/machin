package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// b64url is JWT/JWKS base64url without padding (RFC 7515).
func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// mintRS256 builds a real RS256 JWT: base64url(headerJSON).base64url(payloadJSON)
// signed with PKCS#1 v1.5 over SHA-256 — exactly what an IdP emits for an
// id_token. Returns the compact token string.
func mintRS256(t *testing.T, key *rsa.PrivateKey, headerJSON, payloadJSON string) string {
	t.Helper()
	signingInput := b64url([]byte(headerJSON)) + "." + b64url([]byte(payloadJSON))
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + b64url(sig)
}

// TestJWTVerifyRS256 exercises the pure-MFL RS256 id_token verifier
// (sso_verify_rs256 in framework/sso.src), the "RS256 JWT verification" half of
// issue #484. We mint a genuine RS256 JWT + JWKS in Go (the same crypto/rsa an
// IdP uses) and assert the MFL helper: (1) accepts a valid token and returns its
// claims, (2) rejects a tampered payload, (3) rejects alg-confusion (an HS256 /
// unsigned header — the classic JWT attack), and (4) rejects a token whose kid
// is absent from the JWKS.
func TestJWTVerifyRS256(t *testing.T) {
	mw, err := os.ReadFile("framework/machweb.src")
	if err != nil {
		t.Skip("framework/machweb.src not found")
	}
	sso, err := os.ReadFile("framework/sso.src")
	if err != nil {
		t.Skip("framework/sso.src not found")
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	// JWKS exposes n/e as base64url big-endian bytes.
	nB64 := b64url(key.N.Bytes())
	eBytes := []byte{byte(key.E >> 16), byte(key.E >> 8), byte(key.E)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	eB64 := b64url(eBytes)
	jwks := `{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"k1","n":"` +
		nB64 + `","e":"` + eB64 + `"}]}`

	payload := `{"sub":"u1","email":"ada@x.com","iss":"https://idp.example","exp":9999999999}`
	valid := mintRS256(t, key, `{"alg":"RS256","typ":"JWT","kid":"k1"}`, payload)

	// Tampered: swap in a different payload but keep the original signature.
	vparts := strings.Split(valid, ".")
	tampered := vparts[0] + "." + b64url([]byte(`{"sub":"admin","email":"evil@x.com"}`)) + "." + vparts[2]

	// alg confusion: a header claiming HS256 must be refused before any key use.
	hs256 := mintRS256(t, key, `{"alg":"HS256","typ":"JWT","kid":"k1"}`, payload)

	// Correctly signed, but its kid is not in the JWKS.
	wrongKid := mintRS256(t, key, `{"alg":"RS256","typ":"JWT","kid":"k2"}`, payload)

	// MFL string literals escape only the double quote; our JSON has no backslashes.
	esc := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }

	app := "\n" +
		"func main() {\n" +
		"    jwks := \"" + esc(jwks) + "\"\n" +
		"    claims, ok := sso_verify_rs256(\"" + valid + "\", jwks)\n" +
		"    email, _ := json_get(claims, \".email\")\n" +
		"    println(\"valid=\" + str(ok) + \" email=\" + email)\n" +
		"    _, bad := sso_verify_rs256(\"" + tampered + "\", jwks)\n" +
		"    println(\"tampered=\" + str(bad))\n" +
		"    _, alg := sso_verify_rs256(\"" + hs256 + "\", jwks)\n" +
		"    println(\"algconf=\" + str(alg))\n" +
		"    _, wk := sso_verify_rs256(\"" + wrongKid + "\", jwks)\n" +
		"    println(\"wrongkid=\" + str(wk))\n" +
		"}\n"

	prog, perr := progFromSrcErr(string(mw) + string(sso) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		`valid=1 email="ada@x.com"`, // valid signature -> claims returned
		"tampered=0",                // altered payload -> signature fails
		"algconf=0",                 // HS256 header -> rejected outright
		"wrongkid=0",                // kid absent from JWKS -> no key -> rejected
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
