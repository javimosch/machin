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

// pbkdf2_sha256 must produce deterministic output and derive keys of the requested length.
// The builtin had no direct test coverage — only exercised indirectly through mfl_test.go.
func TestPBKDF2SHA256(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    pwd := bytes("password")
    salt := bytes("salt")
    iterations := 2
    length := 32

    k1 := pbkdf2_sha256(pwd, salt, iterations, length)
    k2 := pbkdf2_sha256(pwd, salt, iterations, length)

    println("len=" + str(len(k1)))
    println("deterministic=" + str(to_hex(k1) == to_hex(k2)))

    k3 := pbkdf2_sha256(pwd, bytes("other"), iterations, length)
    println("salt_matters=" + str(to_hex(k1) != to_hex(k3)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"len=32", "deterministic=true", "salt_matters=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// argon2id/argon2d/argon2i must match the RFC 9106 reference vectors (verified
// against argon2-cffi / the PHC reference implementation). Covers the KDBX4
// password-KDF path from issue #694.
func TestArgon2(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    pwd := bytes("password")
    salt := bytes("somesalt123")

    println(to_hex(argon2id(pwd, salt, 2, 32, 1, 32)))
    println(to_hex(argon2d(pwd, salt, 2, 32, 1, 32)))
    println(to_hex(argon2i(pwd, salt, 1, 64, 2, 32)))
    println("bad_params_empty=" + str(len(argon2id(pwd, bytes("short"), 2, 32, 1, 32)) == 0))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"103298319f4ba264de20dd231623722586c29776640ec40219d0fd8fb01dbdf3",
		"5be130f7b50d39f0cc74440144c0a23393aa2ef0c7a44af51de48a0a921afc68",
		"d59bd7ac3b22bf06bcb49a3448a645bdab247e77d4b0d4d1a04aa75dfd041d23",
		"bad_params_empty=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
