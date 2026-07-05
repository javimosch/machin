package main

import (
	"bytes"
	"compress/gzip"
	"os"
	"testing"
)

// TestGunzip covers the round trip (data survives compress/decompress) and the
// error path (gunzip must reject non-gzip input rather than panic or hang).
func TestGunzip(t *testing.T) {
	want := []byte("hello from the sqlite amalgamation embed, repeated repeated repeated")
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(want); err != nil {
		t.Fatalf("gzip.Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip.Close: %v", err)
	}

	got, err := gunzip(buf.Bytes())
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("gunzip round trip: got %q, want %q", got, want)
	}

	if _, err := gunzip([]byte("not gzip data")); err == nil {
		t.Fatal("gunzip: expected an error for non-gzip input, got nil")
	}
}

// TestCCPath covers both branches of ccPath: the CC env override and the "cc"
// fallback used when CC is unset.
func TestCCPath(t *testing.T) {
	old, hadOld := os.LookupEnv("CC")
	t.Cleanup(func() {
		if hadOld {
			os.Setenv("CC", old)
		} else {
			os.Unsetenv("CC")
		}
	})

	os.Unsetenv("CC")
	if got := ccPath(); got != "cc" {
		t.Fatalf("ccPath with CC unset: got %q, want %q", got, "cc")
	}

	os.Setenv("CC", "musl-gcc")
	if got := ccPath(); got != "musl-gcc" {
		t.Fatalf("ccPath with CC=musl-gcc: got %q, want %q", got, "musl-gcc")
	}
}

// TestZigPath covers both branches of zigPath: the ZIG env override and the
// "zig" fallback used when ZIG is unset.
func TestZigPath(t *testing.T) {
	old, hadOld := os.LookupEnv("ZIG")
	t.Cleanup(func() {
		if hadOld {
			os.Setenv("ZIG", old)
		} else {
			os.Unsetenv("ZIG")
		}
	})

	os.Unsetenv("ZIG")
	if got := zigPath(); got != "zig" {
		t.Fatalf("zigPath with ZIG unset: got %q, want %q", got, "zig")
	}

	os.Setenv("ZIG", "/opt/zig/zig")
	if got := zigPath(); got != "/opt/zig/zig" {
		t.Fatalf("zigPath with ZIG=/opt/zig/zig: got %q, want %q", got, "/opt/zig/zig")
	}
}

// TestCCPathEmpty covers the case where CC is set but empty, which should fall
// back to the default "cc".
func TestCCPathEmpty(t *testing.T) {
	old, hadOld := os.LookupEnv("CC")
	t.Cleanup(func() {
		if hadOld {
			os.Setenv("CC", old)
		} else {
			os.Unsetenv("CC")
		}
	})

	os.Setenv("CC", "")
	if got := ccPath(); got != "cc" {
		t.Fatalf("ccPath with CC empty: got %q, want %q", got, "cc")
	}
}

// TestZigPathEmpty covers the case where ZIG is set but empty, which should fall
// back to the default "zig".
func TestZigPathEmpty(t *testing.T) {
	old, hadOld := os.LookupEnv("ZIG")
	t.Cleanup(func() {
		if hadOld {
			os.Setenv("ZIG", old)
		} else {
			os.Unsetenv("ZIG")
		}
	})

	os.Setenv("ZIG", "")
	if got := zigPath(); got != "zig" {
		t.Fatalf("zigPath with ZIG empty: got %q, want %q", got, "zig")
	}
}
