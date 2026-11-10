package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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

// The deflate/inflate subset of zlib, so the WINDOWS target can compile it in
// rather than requiring a mingw libz the user has to find. Native builds still
// link the system -lz and are unaffected. See vendor/zlib/README.md and #517.
//
//go:embed vendor/zlib/zlib-src.tar.gz
var zlibSrcTarGz []byte

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

// hasStaticLib reports whether the C compiler can find a static archive for
// the given library name (lib<name>.a) in its search path — i.e. the -dev
// package providing it is installed. `cc -print-file-name` echoes the bare
// filename back when nothing matches, so a resolved path means "present".
func hasStaticLib(name string) bool {
	out, err := exec.Command(ccPath(), "-print-file-name=lib"+name+".a").Output()
	if err != nil {
		return false
	}
	p := strings.TrimSpace(string(out))
	return p != "" && p != "lib"+name+".a" && strings.Contains(p, string(os.PathSeparator))
}

// unpackZlibSources extracts the vendored zlib subset into a fresh temp dir and
// returns (dir, the .c files to compile). The caller removes dir.
//
// A tarball rather than SQLite's single gzipped amalgamation because zlib has no
// amalgamation: it is a dozen translation units that expect to be compiled
// separately, and concatenating them is a portability gamble for no benefit.
func unpackZlibSources() (dir string, csrcs []string, err error) {
	dir, err = os.MkdirTemp("", "mfl-zlib-*")
	if err != nil {
		return "", nil, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(zlibSrcTarGz))
	if err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("zlib sources: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(dir)
			return "", nil, fmt.Errorf("zlib sources: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Flatten: the archive is a flat list of .c/.h, and refusing any path
		// separator keeps a crafted archive from writing outside dir.
		name := filepath.Base(hdr.Name)
		if name != hdr.Name {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			os.RemoveAll(dir)
			return "", nil, fmt.Errorf("zlib sources: %w", err)
		}
		out := filepath.Join(dir, name)
		if err := os.WriteFile(out, data, 0o644); err != nil {
			os.RemoveAll(dir)
			return "", nil, err
		}
		if strings.HasSuffix(name, ".c") {
			csrcs = append(csrcs, out)
		}
	}
	sort.Strings(csrcs) // deterministic command line
	return dir, csrcs, nil
}

// unpackSqliteAmalgamation decompresses the embedded SQLite amalgamation into a
// fresh temp dir and returns (dir, path-to-sqlite3.c). The caller removes dir.
//
// Shared by the --static native path and the windows target: SQLite has its own
// Win32 VFS and machin's glue is pure sqlite3 API with no POSIX in it, so the
// same amalgamation compiles for both — bundling is the whole port (#517).
func unpackSqliteAmalgamation() (dir string, cpath string, err error) {
	dir, err = os.MkdirTemp("", "mfl-sqlite-*")
	if err != nil {
		return "", "", err
	}
	hdr, err := gunzip(sqliteHdrGz)
	if err != nil {
		os.RemoveAll(dir)
		return "", "", fmt.Errorf("sqlite amalgamation: %w", err)
	}
	amalg, err := gunzip(sqliteAmalgGz)
	if err != nil {
		os.RemoveAll(dir)
		return "", "", fmt.Errorf("sqlite amalgamation: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sqlite3.h"), hdr, 0o644); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	cpath = filepath.Join(dir, "sqlite3.c")
	if err := os.WriteFile(cpath, amalg, 0o644); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	return dir, cpath, nil
}

// sqliteBundleFlags are the compile flags for a bundled amalgamation: point the
// generated `#include <sqlite3.h>` at our header, drop extension loading (no
// dlopen, so the binary stays self-contained), and keep it threadsafe for
// machweb's per-connection goroutines.
func sqliteBundleFlags(dir string) []string {
	return []string{"-I" + dir, "-DSQLITE_OMIT_LOAD_EXTENSION", "-DSQLITE_THREADSAFE=1"}
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
	usesZlib := strings.Contains(csrc, "mfl_zlib_")
	bundleSqlite := static && usesSqlite

	// foreign linkage: extern cflags go before the source; -l libs after it
	// -fno-strict-aliasing: the raw-memory peek_*/poke_* builtins (and other
	// runtime helpers) type-pun through pointer casts, which -O2's strict
	// aliasing otherwise treats as UB and can silently miscompile.
	// -fwrapv: signed overflow wraps two's-complement (matching Go's spec)
	// instead of being UB that -O2 is free to fold into a constant.
	args := []string{"-O2", "-fno-strict-aliasing", "-fwrapv", "-std=c11", "-pthread"}
	var srcs []string // extra C source files compiled alongside the generated one
	if static {
		args = append(args, "-static")
	}
	if bundleSqlite {
		dir, cpath, err := unpackSqliteAmalgamation()
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		args = append(args, sqliteBundleFlags(dir)...)
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
			"provides them, no musl needed for this part, unlike --static SQLite). " +
			"Depending on how the distro built OpenSSL, its archives may also need " +
			"zlib1g-dev / libzstd-dev / libbrotli*-dev / libjitterentropy3-dev " +
			"for the helper symbols (machin links whichever static archives it finds)."
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
	// zlib compression (zlib_compress/zlib_decompress) links libz — only when used.
	if usesZlib {
		libs = append(libs, "-lz")
	}
	// OpenCL GPU backend (mfl_ocl_*) dynamically loads libOpenCL via dlopen on
	// Linux — needs -ldl (harmless on glibc ≥2.34 where it's a stub, required below).
	if strings.Contains(csrc, "mfl_ocl_init") && runtime.GOOS == "linux" {
		libs = append(libs, "-ldl")
	}
	// Static OpenSSL archives are built differently per distro: some libcrypto.a
	// objects reference compression libs (zlib's inflate/deflate, zstd's
	// ZSTD_*, brotli's Brotli*) and non-compression helpers (jitterentropy's
	// jent_* on OpenSSL 3.5+, dlopen) — dynamic links resolve those
	// transitively through libssl.so's own deps; static links must name them
	// explicitly — and AFTER the crypto libs, since ld resolves
	// left-to-right. Which ones a given libcrypto.a needs varies by distro
	// build, so probe for each static archive and add only what the host
	// actually has — an unconditional -l would break --static TLS on hosts
	// without the -dev packages even when their OpenSSL doesn't need them.
	// Unused probed libs are harmless: ld pulls nothing from an archive that
	// resolves no undefined symbol.
	if static && (usesTLS || usesCrypto || usesSodium) {
		for _, l := range []string{"z", "zstd", "brotlidec", "brotlienc", "brotlicommon", "jitterentropy"} {
			if hasStaticLib(l) {
				libs = append(libs, "-l"+l)
			}
		}
		if runtime.GOOS == "linux" {
			libs = append(libs, "-ldl")
		}
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
	// -fwrapv: signed overflow wraps two's-complement (matching Go's spec)
	// instead of being UB that -O2 is free to fold into a constant.
	args := []string{"cc", "-target", "wasm32-wasi", "-mexec-model=reactor", "-O2", "-fno-strict-aliasing", "-fwrapv", "-std=c11", "-o", outPath, tmp.Name()}

	cmd := exec.Command(zigPath(), args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\n%s", zigPath(), err, out)
	}
	return nil
}

// BuildWindows cross-compiles the program to a Windows x86-64 .exe via
// `zig cc -target x86_64-windows-gnu` (mingw-w64 + winpthreads, a single-binary
// cross toolchain like the wasm path). Covers #517: the POSIX-independent core,
// TCP sockets (winsock2), HTTPS/TLS + OpenSSL crypto builtins, terminal raw
// mode, SQLite and zlib (both vendored in), and XEdDSA. The preflight still
// rejects POSIX regex only.
//
// zig alone suffices EXCEPT for TLS/crypto/XEdDSA, which link OpenSSL/libsodium:
// zig does not ship these for the windows-gnu target, so the caller must point
// MACHIN_WIN_OPENSSL at a mingw-w64 OpenSSL install (a dir with include/ and lib/
// holding libssl.a / libcrypto.a, e.g. from msys2's ucrt64 environment —
// mingw-w64-ucrt-x86_64-openssl; zig's windows-gnu target is UCRT, so the
// msvcrt-flavored mingw64 packages fail at link with dllimport errors) and, for
// xeddsa_*, MACHIN_WIN_SODIUM at a mingw-w64 libsodium (a dir with lib/
// holding libsodium.a — the runtime declares the symbols itself, so no headers
// are needed). This mirrors how the native build already requires libssl-dev
// and libsodium-dev on the build host. Override the zig binary with $ZIG.
func BuildWindows(prog *Program, outPath string, safe bool) error {
	csrc, _, err := CompileToCTarget(prog, safe, targetWindows)
	if err != nil {
		return err
	}

	usesTLS := strings.Contains(csrc, "mfl_tls_dial")
	usesCrypto := strings.Contains(csrc, "mfl_crypto_")
	usesNet := strings.Contains(csrc, "WSAStartup")
	usesSqlite := strings.Contains(csrc, "mfl_sqlite_")
	usesZlib := strings.Contains(csrc, "mfl_zlib_")
	usesSodium := strings.Contains(csrc, "mfl_xeddsa_")
	// XEdDSA hashes with OpenSSL SHA-512, so it needs OpenSSL too.
	usesOpenSSL := usesTLS || usesCrypto || usesSodium

	// TLS/crypto/XEdDSA need a mingw OpenSSL the caller supplies out of band.
	sslDir := os.Getenv("MACHIN_WIN_OPENSSL")
	if usesOpenSSL && sslDir == "" {
		return fmt.Errorf("the windows target needs an OpenSSL built for x86_64-windows-gnu to link this program's TLS/crypto/XEdDSA: set MACHIN_WIN_OPENSSL=/path to a mingw OpenSSL (a dir with include/ and lib/ — e.g. msys2's ucrt64 mingw-w64-ucrt-x86_64-openssl; zig's windows-gnu target is UCRT, so msvcrt mingw64 builds fail to link). See issue #517")
	}

	// XEdDSA additionally needs a mingw libsodium the caller supplies out of band.
	sodiumDir := ""
	if usesSodium {
		sodiumDir = os.Getenv("MACHIN_WIN_SODIUM")
		if sodiumDir == "" {
			return fmt.Errorf("the windows target needs a libsodium built for x86_64-windows-gnu to link this program's XEdDSA builtins: set MACHIN_WIN_SODIUM=/path to a mingw libsodium (a dir with lib/ holding libsodium.a — e.g. msys2's ucrt64 mingw-w64-ucrt-x86_64-libsodium). See issue #517")
		}
	}

	tmp, err := os.CreateTemp("", "mfl-*.c")
	if err != nil {
		return err
	}
	// defer os.Remove(tmp.Name()) // DEBUG: keep temp C file
	if _, err := tmp.WriteString(csrc); err != nil {
		return err
	}
	tmp.Close()
	fmt.Fprintf(os.Stderr, "DEBUG: C source at %s\n", tmp.Name())

	// libm (math/noise) is in mingw's default libs; winpthreads is linked
	// automatically by zig for the *-windows-gnu target, so no explicit -l.
	// -fwrapv: signed overflow wraps two's-complement (matching Go's spec)
	// instead of being UB that -O2 is free to fold into a constant.
	args := []string{"cc", "-target", "x86_64-windows-gnu", "-O2", "-fno-strict-aliasing", "-fwrapv", "-std=c11"}
	var srcs []string // extra C sources compiled alongside the generated one
	if usesOpenSSL {
		args = append(args, "-I"+filepath.Join(sslDir, "include"))
	}
	// A TLS program needs a CA root store to verify certificates; Windows has no
	// /etc/ssl/certs, so compile the vendored CA bundle in (as the native --static
	// build does) and tell mfl_ssl_ctx via -DMFL_HAS_CABUNDLE.
	if usesTLS {
		dir, err := os.MkdirTemp("", "mfl-win-ca-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		pem, err := gunzip(caBundleGz)
		if err != nil {
			return fmt.Errorf("ca bundle: %w", err)
		}
		cpath := filepath.Join(dir, "mfl_cabundle.c")
		if err := os.WriteFile(cpath, []byte("#include <stddef.h>\n"+cBytesLiteral("mfl_ca_bundle_pem", pem)), 0o644); err != nil {
			return err
		}
		args = append(args, "-DMFL_HAS_CABUNDLE")
		srcs = append(srcs, cpath)
	}
	// SQLite: same bundled amalgamation the --static native build uses. It carries
	// its own Win32 VFS, so nothing else is needed — no external library, no DLL.
	if usesSqlite {
		dir, cpath, err := unpackSqliteAmalgamation()
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		args = append(args, sqliteBundleFlags(dir)...)
		srcs = append(srcs, cpath)
	}
	// zlib: the vendored deflate/inflate subset, compiled in. Windows has no
	// system libz, and requiring a mingw one would push a dependency onto every
	// program that calls zlib_compress.
	if usesZlib {
		dir, csrcs, err := unpackZlibSources()
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		args = append(args, "-I"+dir)
		srcs = append(srcs, csrcs...)
	}
	args = append(args, "-o", outPath, tmp.Name())
	args = append(args, srcs...)
	args = append(args, windowsLinkLibs(usesTLS, usesOpenSSL, usesSodium, usesNet, sslDir, sodiumDir)...)
	cmd := exec.Command(zigPath(), args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\n%s", zigPath(), err, out)
	}
	return nil
}

// windowsLinkLibs returns the -L/-l tail of the `zig cc` command for a windows
// build, given which subsystems the emitted C uses. -lssl is only added when
// TLS is actually used: crypto/XEdDSA need libcrypto alone. libsodium needs
// OpenSSL too (XEdDSA hashes with EVP SHA-512), so usesSodium implies
// usesOpenSSL. System libs: ws2_32 for sockets/TLS; crypt32/bcrypt for
// OpenSSL's crypto and libsodium's randomness; advapi32 for libsodium's RNG
// fallback.
func windowsLinkLibs(usesTLS, usesOpenSSL, usesSodium, usesNet bool, sslDir, sodiumDir string) []string {
	var args []string
	if usesOpenSSL {
		args = append(args, "-L"+filepath.Join(sslDir, "lib"))
		if usesTLS {
			args = append(args, "-lssl")
		}
		args = append(args, "-lcrypto")
	}
	if usesSodium {
		args = append(args, "-L"+filepath.Join(sodiumDir, "lib"), "-lsodium")
	}
	if usesNet || usesOpenSSL {
		args = append(args, "-lws2_32")
	}
	if usesOpenSSL {
		args = append(args, "-lcrypt32", "-lbcrypt", "-ladvapi32")
	}
	return args
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
	// THE CHILD'S STDERR IS PART OF THE FAILURE, and Output() already collected
	// it — into ExitError.Stderr, where nothing ever looked. Printing the error
	// with %v yields a bare "signal: aborted (core dumped)": no glibc message,
	// no deadlock report, no sanitizer trace, no "bind: Address already in
	// use". For a flake, where reproducing it is the one thing you cannot do,
	// that is the difference between a diagnosis and a shrug.
	//
	// Note this deliberately does NOT set cmd.Stderr: Output already captures
	// stderr into ExitError.Stderr, and setting it replaces that with a second
	// pipe this function would have to drain itself, for no gain.
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		err = fmt.Errorf("%w; stderr:\n%s", err, strings.TrimRight(string(ee.Stderr), "\n"))
	}
	return string(out), err
}
