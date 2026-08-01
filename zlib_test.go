package main

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

// zlib_compress/zlib_decompress are the raw zlib codec the market-data collector
// needs for its SQLite BLOBs. We verify both round-trip and interoperability
// with Go's compress/zlib (which is the downstream consumer's format).
func TestZlibRoundTripAndGoInterop(t *testing.T) {
	original := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog. "), 20)
	wantHex := hex.EncodeToString(original)

	prog := progFromSrc(t, `
func main() {
	b := from_hex("`+wantHex+`")
	c := zlib_compress(b, 6)
	d := zlib_decompress(c)
	println("comp=" + to_hex(c))
	println("orig=" + to_hex(b))
	println("decomp=" + to_hex(d))
	println("lens=" + str(len(c)) + " " + str(len(d)))
}`)

	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if p := strings.Index(line, "="); p >= 0 {
			m[line[:p]] = line[p+1:]
		}
	}

	if m["orig"] != wantHex {
		t.Fatalf("original mismatch: got %q, want %q", m["orig"], wantHex)
	}
	if m["decomp"] != wantHex {
		t.Fatalf("decompress mismatch: got %q, want %q", m["decomp"], wantHex)
	}

	compRaw, err := hex.DecodeString(m["comp"])
	if err != nil {
		t.Fatalf("decode comp hex: %v", err)
	}
	if len(compRaw) >= len(original) {
		t.Fatalf("compressed length %d should be smaller than original %d", len(compRaw), len(original))
	}

	// Go's compress/zlib must be able to decompress what MFL produced.
	r, err := zlib.NewReader(bytes.NewReader(compRaw))
	if err != nil {
		t.Fatalf("zlib.NewReader on MFL output: %v", err)
	}
	goOut, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("zlib read: %v", err)
	}
	r.Close()
	if !bytes.Equal(goOut, original) {
		t.Fatalf("Go zlib decompression of MFL output differs: got %q, want %q", goOut, original)
	}

	// And MFL must decompress what Go produced.
	var goCompressed bytes.Buffer
	w, err := zlib.NewWriterLevel(&goCompressed, 6)
	if err != nil {
		t.Fatalf("zlib.NewWriterLevel: %v", err)
	}
	w.Write(original)
	w.Close()

	goCompHex := hex.EncodeToString(goCompressed.Bytes())
	prog2 := progFromSrc(t, `
func main() {
	b := from_hex("`+goCompHex+`")
	println(to_hex(zlib_decompress(b)))
}`)
	out2, err := RunCaptured(prog2)
	if err != nil {
		t.Fatalf("run MFL decompress of Go zlib: %v", err)
	}
	if got := strings.TrimSpace(out2); got != wantHex {
		t.Fatalf("MFL decompress of Go zlib: got %q, want %q", got, wantHex)
	}
}

// Empty input round-trips, and bad data yields empty bytes.
func TestZlibEdgeCases(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
	empty := bytes("")
	c := zlib_compress(empty, 6)
	d := zlib_decompress(c)
	bad := zlib_decompress(from_hex("deadbeef"))
	println(str(len(c)) + " " + str(len(d)) + " " + str(len(bad)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), " 0 0") {
		t.Fatalf("expected some compressed length, 0 decompressed, 0 bad; got %q", out)
	}
}
