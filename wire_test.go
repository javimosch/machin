package main

import (
	"strings"
	"testing"
)

// base64_encode_bytes / base64_decode_bytes are binary-safe (unlike the string
// forms, which stop at a NUL) — the primitive SCRAM (and any binary token) needs.
func TestBase64BytesRoundTrip(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    b := from_hex("00ff00deadbeef00")          // leading + embedded + trailing NUL
    enc := base64_encode_bytes(b)
    dec := base64_decode_bytes(enc)
    println("enc=" + enc)
    println("rt=" + to_hex(dec) + " len=" + str(len(dec)))
    m := 0
    if base64_encode_bytes(bytes("hello")) == base64_encode("hello") { m = 1 }   // ASCII matches the string form
    println("ascii_match=" + str(m))
    println("empty=[" + base64_encode_bytes(from_hex("")) + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"enc=AP8A3q2+7wA=",
		"rt=00ff00deadbeef00 len=8", // round-trips all 8 bytes incl. the NULs
		"ascii_match=1",
		"empty=[]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// read_bytes is the NUL-safe socket read (read() returns a C string, truncating at
// a NUL). In-process loopback: a goroutine writes binary, the client read_bytes it.
func TestReadBytesBinarySocket(t *testing.T) {
	prog := progFromSrc(t, `
func serve_one(srv) { conn := accept(srv)  write_bytes(conn, from_hex("00ff0042"))  close(conn) }
func main() {
    srv := listen(48234)
    go serve_one(srv)
    sleep(50)
    c := dial("127.0.0.1", 48234)
    rb := read_bytes(c)
    println("len=" + str(len(rb)) + " b0=" + str(byte_at(rb,0)) + " b1=" + str(byte_at(rb,1)) + " b3=" + str(byte_at(rb,3)))
    close(c)
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// len=4 (not truncated at the leading NUL), and the NUL byte itself read as 0
	if !strings.Contains(out, "len=4 b0=0 b1=255 b3=66") {
		t.Fatalf("read_bytes should return all 4 bytes incl. the leading NUL; got:\n%s", out)
	}
}
