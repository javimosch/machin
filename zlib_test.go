package main

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestZlibRoundTrip verifies that zlib_compress -> zlib_decompress is lossless
// and works at the level the issue calls out (6).
func TestZlibRoundTrip(t *testing.T) {
	src := `func main(){
		in := bytes("orderbook bids and asks")
		c := zlib_compress(in, 6)
		out := zlib_decompress(c)
		println(bytes_str(out))
	}`
	if got := runNative(t, src); got != "orderbook bids and asks\n" {
		t.Fatalf("round-trip: got %q", got)
	}
}

// TestZlibEmpty verifies that the empty input round-trips cleanly. zlib_compress
// of empty data produces a short, valid stream; zlib_decompress of it returns
// an empty bytes value.
func TestZlibEmpty(t *testing.T) {
	src := `func main(){
		c := zlib_compress(bytes(""), 6)
		out := zlib_decompress(c)
		println(len(c), len(out))
	}`
	got := runNative(t, src)
	parts := strings.Fields(strings.TrimSpace(got))
	if len(parts) != 2 {
		t.Fatalf("expected 2 fields, got %q", got)
	}
	if parts[0] == "0" || parts[1] != "0" {
		t.Fatalf("empty compress produced 0-length or empty did not round-trip: got %q", got)
	}
}

// TestZlibLevel0And9 verifies the level parameter is honored across the range.
func TestZlibLevels(t *testing.T) {
	for _, level := range []int{0, 1, 6, 9} {
		src := fmt.Sprintf(`func main(){
			in := bytes("level %d test payload")
			c := zlib_compress(in, %d)
			out := zlib_decompress(c)
			println(bytes_str(out))
		}`, level, level)
		want := fmt.Sprintf("level %d test payload\n", level)
		if got := runNative(t, src); got != want {
			t.Fatalf("level %d: got %q, want %q", level, got, want)
		}
	}
}

// TestZlibCompressGoDecompress verifies MFL's zlib_compress output can be
// decompressed by Go's compress/zlib (the exact cross-language round-trip the
// issue needs for the collector's existing blobs).
func TestZlibCompressGoDecompress(t *testing.T) {
	src := `func main(){
		in := bytes("MFL compress -> Go compress/zlib decompress")
		c := zlib_compress(in, 6)
		println(to_hex(c))
	}`
	got := strings.TrimSpace(runNative(t, src))
	b, err := hex.DecodeString(got)
	if err != nil {
		t.Fatalf("hex decode %q: %v", got, err)
	}
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("zlib reader: %v", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) != "MFL compress -> Go compress/zlib decompress" {
		t.Fatalf("got %q", out)
	}
}

// TestZlibDecompressGoCompress verifies MFL can decompress a stream produced by
// Go's compress/zlib at level 6 (the collector's existing on-disk format).
func TestZlibDecompressGoCompress(t *testing.T) {
	input := []byte("Go compress/zlib compress -> MFL decompress")
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, 6)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	if _, err := w.Write(input); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	src := fmt.Sprintf(`func main(){
		z := from_hex("%s")
		out := zlib_decompress(z)
		println(bytes_str(out))
	}`, hex.EncodeToString(buf.Bytes()))
	if got := runNative(t, src); got != string(input)+"\n" {
		t.Fatalf("MFL decompress of Go stream: got %q", got)
	}
}

// TestZlibInvalidInput verifies zlib_decompress returns empty bytes on an
// invalid stream instead of crashing.
func TestZlibInvalidInput(t *testing.T) {
	src := `func main(){
		out := zlib_decompress(bytes("not a zlib stream"))
		println(len(out))
	}`
	if got := runNative(t, src); got != "0\n" {
		t.Fatalf("invalid input: got %q", got)
	}
}
