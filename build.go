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

// A CA root bundle (Mozilla's store, as shipped by Debian/Ubuntu), gzipped and
// embedded so `machin build --static` can compile it directly into a TLS-using
// program — a static binary can then verify server certificates with zero
// external files (no system CA store needed), so it runs FROM scratch. See
// vendor/certs/README.md and issue #283.
//
//go:embed vendor/certs/cacert.pem.gz
var caBundleGz []byte

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// cBytesLiteral renders data as a C translation unit defining a byte array
// `name` and a `name_len` length constant — the standard way to embed an
// arbitrary binary blob (here, a CA bundle) directly into a compiled program.
func cBytesLiteral(name string, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "const unsigned char %s[] = {\n", name)
	for i, by := range data {
		if i%20 == 0 {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "0x%02x,", by)
		if i%20 == 19 {
			b.WriteByte('\n')
		}
	}
	b.WriteString("\n};\n")
	fmt.Fprintf(&b, "const unsigned long %s_len = %dUL;\n", name, len(data))
	return b.String()
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
// that runs FROM scratch. A TLS-using program gets a static OpenSSL link (needs
// libssl-dev's static archives on the build host, no musl required) plus an
// embedded CA root bundle, so it can verify certificates FROM scratch too — see
// vendor/certs/README.md and issue #283. Server-side TLS/STARTTLS is a separate,
// still-open capability gap — see issue #260.
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
	// -fno-strict-aliasing: the raw-memory peek_*/poke_* builtins (and other
	// runtime helpers) type-pun through pointer casts, which -O2's strict
	// aliasing otherwise treats as UB and can silently miscompile.
	args := []string{"-O2", "-fno-strict-aliasing", "-std=c11", "-pthread"}
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
	bundleCABundle := static && usesTLS
	if bundleCABundle {
		// Decompress the embedded CA bundle, compile it in as a byte array, and tell
		// mfl_ssl_ctx() (codegen.go) it exists via -DMFL_HAS_CABUNDLE — so a static
		// binary can verify server certificates with no external CA store. See
		// vendor/certs/README.md and issue #283.
		dir, err := os.MkdirTemp("", "mfl-cabundle-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		pem, err := gunzip(caBundleGz)
		if err != nil {
			return fmt.Errorf("ca bundle: %w", err)
		}
		cpath := filepath.Join(dir, "mfl_cabundle.c")
		src := "#include <stddef.h>\n" + cBytesLiteral("mfl_ca_bundle_pem", pem)
		if err := os.WriteFile(cpath, []byte(src), 0o644); err != nil {
			return err
		}
		args = append(args, "-DMFL_HAS_CABUNDLE")
		srcs = append(srcs, cpath)
	}
	if static && (usesTLS || usesCrypto || usesSodium) {
		note := "machin: --static + TLS/crypto: linking a static OpenSSL (needs libssl-dev's " +
			"static archives — .a files — on the build host; a plain `apt install libssl-dev` " +
			"provides them, no musl needed for this part, unlike --static SQLite)."
		if bundleCABundle {
			note += " Bundling a CA root store so the binary verifies certificates with no " +
				"external files (FROM scratch)."
		}
		fmt.Fprintln(os.Stderr, note)
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
	args := []string{"cc", "-target", "wasm32-wasi", "-mexec-model=reactor", "-O2", "-fno-strict-aliasing", "-std=c11", "-o", outPath, tmp.Name()}

	cmd := exec.Command(zigPath(), args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\n%s", zigPath(), err, out)
	}
	return nil
}

// BuildWindows cross-compiles the program to a Windows x86-64 .exe via
// `zig cc -target x86_64-windows-gnu` (mingw-w64 + winpthreads, a single-binary
// cross toolchain like the wasm path). Phase 0 of #517: the POSIX-independent
// core only — CompileToCTarget's windows preflight rejects programs that use
// networking, TLS/crypto, terminal raw mode, SQLite, or regex. No C compiler
// besides zig is needed; override the zig binary with $ZIG.
func BuildWindows(prog *Program, outPath string, safe bool) error {
	csrc, _, err := CompileToCTarget(prog, safe, targetWindows)
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

	// libm (math/noise) is in mingw's default libs; winpthreads is linked
	// automatically by zig for the *-windows-gnu target, so no explicit -l.
	args := []string{"cc", "-target", "x86_64-windows-gnu", "-O2", "-fno-strict-aliasing", "-std=c11", "-o", outPath, tmp.Name()}
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
