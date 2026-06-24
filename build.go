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
	// native math (sin/cos/sqrt/...) and Perlin noise both link libm — only when used.
	if strings.Contains(csrc, "mfl_math_") || strings.Contains(csrc, "mfl_noise") {
		libs = append(libs, "-lm")
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

// zigPath is the C->wasm cross compiler. zig ships clang + a wasi-libc, so it is
// a single-binary toolchain (no emscripten / wasi-sdk needed). Override with ZIG.
func zigPath() string {
	if z := os.Getenv("ZIG"); z != "" {
		return z
	}
	return "zig"
}

// BuildWasm compiles the program to a WebAssembly module at outPath, targeting
// wasm32-wasi in *reactor* model (no _start: the host drives it through the
// exported functions). The emitted C tags FFI imports as wasm imports and omits
// the POSIX socket/tty runtime unless used, so a frontend module loads in a bare
// browser with only its `extern "env"` host functions as imports. Each `export
// func` is handed to the linker as a wasm export.
func BuildWasm(prog *Program, outPath string, safe bool) error {
	csrc, exports, err := CompileToCTarget(prog, safe, targetWasm)
	if err != nil {
		return err
	}
	if len(exports) == 0 {
		return fmt.Errorf("wasm target: no exported functions — mark at least one with `export func`")
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

	// Each exported function carries an export_name attribute (emitted by codegen),
	// which forces its export under the clean source name — so no --export flags.
	_ = exports
	args := []string{"cc", "-target", "wasm32-wasi", "-mexec-model=reactor", "-O2", "-std=c11", "-o", outPath, tmp.Name()}

	cmd := exec.Command(zigPath(), args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\n%s", zigPath(), err, out)
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
