package main

import (
	"strings"
	"testing"
)

// aes_cbc_encrypt/aes_cbc_decrypt and hkdf_sha256 had no test coverage at
// all — round-trip the AES pair and check hkdf_sha256 produces a
// deterministic, length-correct output.
func TestCryptoBuiltins(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    key := from_hex("000102030405060708090a0b0c0d0e0f")
    iv := from_hex("101112131415161718191a1b1c1d1e1f")
    pt := bytes("hello aes cbc world")
    ct := aes_cbc_encrypt(key, iv, pt)
    rt := aes_cbc_decrypt(key, iv, ct)
    println("roundtrip=" + str(bytes_str(rt) == "hello aes cbc world"))
    println("ctlen=" + str(len(ct) >= len(pt)))

    ikm := bytes("input key material")
    salt := bytes("salt")
    info := bytes("info")
    k1 := hkdf_sha256(ikm, salt, info, 32)
    k2 := hkdf_sha256(ikm, salt, info, 32)
    println("hkdflen=" + str(len(k1)))
    println("hkdfdeterministic=" + str(to_hex(k1) == to_hex(k2)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"roundtrip=true", "ctlen=true", "hkdflen=32", "hkdfdeterministic=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
