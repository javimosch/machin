package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The SQLite amalgamation (public domain, single-file build), gzipped and embedded
// so `machin build --static` can compile it directly into a program — no system
// libsqlite3, runs FROM scratch. See vendor/sqlite/README.md.
//
//go:embed vendor/sqlite/sqlite3.c.gz
var sqliteAmalgGz []byte

//go:embed vendor/sqlite/sqlite3.h.gz
var sqliteHdrGz []byte

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

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
	return BuildBinaryStatic(prog, outPath, safe, false)
}

// BuildBinaryStatic is BuildBinary plus an optional fully-static link (`-static`).
// In static mode, a SQLite-using program gets the embedded amalgamation compiled
// directly in (no libsqlite3), so with `CC=musl-gcc` it produces a libc-free binary
// that runs FROM scratch. TLS/crypto (OpenSSL) is not bundled — see issue #260.
func BuildBinaryStatic(prog *Program, outPath string, safe, static bool) error {
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

	usesSqlite := strings.Contains(csrc, "mfl_sqlite_")
	usesTLS := strings.Contains(csrc, "mfl_tls_dial")
	usesCrypto := strings.Contains(csrc, "mfl_crypto_")
	usesSodium := strings.Contains(csrc, "mfl_xeddsa_")
	bundleSqlite := static && usesSqlite

	// foreign linkage: extern cflags go before the source; -l libs after it
	args := []string{"-O2", "-std=c11", "-pthread"}
	var srcs []string // extra C source files compiled alongside the generated one
	if static {
		args = append(args, "-static")
	}
	if bundleSqlite {
		// Decompress the embedded amalgamation into a temp dir, compile sqlite3.c in,
		// and point the generated `#include <sqlite3.h>` at our bundled header.
		dir, err := os.MkdirTemp("", "mfl-sqlite-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		hdr, err := gunzip(sqliteHdrGz)
		if err != nil {
			return fmt.Errorf("sqlite amalgamation: %w", err)
		}
		amalg, err := gunzip(sqliteAmalgGz)
		if err != nil {
			return fmt.Errorf("sqlite amalgamation: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sqlite3.h"), hdr, 0o644); err != nil {
			return err
		}
		cpath := filepath.Join(dir, "sqlite3.c")
		if err := os.WriteFile(cpath, amalg, 0o644); err != nil {
			return err
		}
		args = append(args, "-I"+dir,
			"-DSQLITE_OMIT_LOAD_EXTENSION", // no dlopen -> no -ldl, stays static
			"-DSQLITE_THREADSAFE=1")        // safe across machweb's per-connection goroutines
		srcs = append(srcs, cpath)
	}
	if static && (usesTLS || usesCrypto || usesSodium) {
		fmt.Fprintln(os.Stderr, "machin: --static note: this program uses TLS/crypto, which "+
			"link OpenSSL/libsodium — machin bundles only SQLite statically. The link needs "+
			"static OpenSSL libs present, or it will fail. Native TLS (no OpenSSL) is tracked "+
			"in issue #260.")
	}

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
	if usesTLS {
		libs = append(libs, "-lssl", "-lcrypto")
	}
	// native SQLite (sqlite_*) links libsqlite3 — only when used, and not when the
	// amalgamation is bundled (static mode), which provides it from source instead.
	if usesSqlite && !bundleSqlite {
		libs = append(libs, "-lsqlite3")
	}
	// native math (sin/cos/sqrt/...) and Perlin noise both link libm — only when used.
	if strings.Contains(csrc, "mfl_math_") || strings.Contains(csrc, "mfl_noise") {
		libs = append(libs, "-lm")
	}
	// crypto builtins (rand/sha/hmac/hkdf/x25519/ed25519/aes) link OpenSSL
	// libcrypto — only when used. Harmless if -lcrypto is already added for TLS.
	if usesCrypto {
		libs = append(libs, "-lcrypto")
	}
	// XEdDSA builtins (xeddsa_sign/verify) link libsodium + OpenSSL libcrypto.
	// Requires libsodium-dev (provides libsodium.so) on the build host.
	if usesSodium {
		libs = append(libs, "-lsodium", "-lcrypto")
	}
	args = append(args, "-o", outPath, tmp.Name())
	args = append(args, srcs...)
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
