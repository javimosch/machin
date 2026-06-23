package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ccPath is the C compiler used to turn emitted C into a native binary.
func ccPath() string {
	if cc := os.Getenv("CC"); cc != "" {
		return cc
	}
	return "cc"
}

// BuildBinary compiles the program to a native executable at outPath via cc -O2.
// When safe is set, runtime bounds, division-by-zero, and overflow checks are
// inserted.
func BuildBinary(prog *Program, outPath string, safe bool) error {
	csrc, err := CompileToC(prog, safe)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "mfl-*.c")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(csrc); err != nil {
		return err
	}
	tmp.Close()

	// foreign linkage: extern cflags go before the source; -l libs after it
	args := []string{"-O2", "-std=c11", "-pthread"}
	var libs []string
	for _, ed := range prog.Externs {
		if ed.CFlags != "" {
			args = append(args, strings.Fields(ed.CFlags)...)
		}
		for _, l := range ed.Links {
			libs = append(libs, "-l"+l)
		}
	}
	// native TLS (https_* / wss_*) links OpenSSL — only when actually used, so
	// TLS-free programs keep machin's libc-only footprint. mfl_tls_dial is in the
	// shared TLS core, emitted whenever either the HTTPS or WSS runtime is used.
	if strings.Contains(csrc, "mfl_tls_dial") {
		libs = append(libs, "-lssl", "-lcrypto")
	}
	// native SQLite (sqlite_*) links libsqlite3 — only when actually used.
	if strings.Contains(csrc, "mfl_sqlite_") {
		libs = append(libs, "-lsqlite3")
	}
	// crypto builtins (rand/sha/hmac/hkdf/x25519/ed25519/aes) link OpenSSL
	// libcrypto — only when used. Harmless if -lcrypto is already added for TLS.
	if strings.Contains(csrc, "mfl_crypto_") {
		libs = append(libs, "-lcrypto")
	}
	// XEdDSA builtins (xeddsa_sign/verify) link libsodium + OpenSSL libcrypto.
	// Requires libsodium-dev (provides libsodium.so) on the build host.
	if strings.Contains(csrc, "mfl_xeddsa_") {
		libs = append(libs, "-lsodium", "-lcrypto")
	}
	args = append(args, "-o", outPath, tmp.Name())
	args = append(args, libs...)

	cmd := exec.Command(ccPath(), args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\n%s", ccPath(), err, out)
	}
	return nil
}

// RunCaptured builds the program to a temp binary, runs it, and returns stdout.
func RunCaptured(prog *Program) (string, error) {
	bin, err := os.CreateTemp("", "mfl-bin-*")
	if err != nil {
		return "", err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		return "", err
	}
	out, err := exec.Command(bin.Name()).Output()
	return string(out), err
}
