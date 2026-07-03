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

// #91: `read`/`read_bytes` are each one read(2) of at most 65535 bytes, not a
// whole message — a payload bigger than that (a large HTTP body, or one that
// simply arrives in more than one TCP segment) needs the CALLER to loop.
// examples/complex/json_echo_api.mfl's read_request does exactly this (loop
// read_bytes until Content-Length bytes are in hand, mirroring
// framework/machweb.src's read_request_bytes) — this test proves the
// underlying mechanism a single read_bytes call cannot: a server writes a
// payload well over the 65535-byte single-read cap, and the client must call
// read_bytes more than once to reassemble it all.
func TestReadBytesLoopReassemblesLargePayload(t *testing.T) {
	prog := progFromSrc(t, `
func serve_big(srv, n) {
    conn := accept(srv)
    payload := bytes("")
    i := 0
    chunk := from_hex("41")   // one byte, 'A'
    while i < n {
        payload = bytes_concat(payload, chunk)
        i = i + 1
    }
    write_bytes(conn, payload)
    close(conn)
}
func main() {
    n := 100000   // well over the 65535-byte single-read(2) cap
    srv := listen(48235)
    go serve_big(srv, n)
    sleep(100)
    c := dial("127.0.0.1", 48235)
    total := bytes("")
    reads := 0
    for {
        chunk := read_bytes(c)
        if len(chunk) == 0 { break }
        total = bytes_concat(total, chunk)
        reads = reads + 1
    }
    close(c)
    println("len=" + str(len(total)) + " reads=" + str(reads))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "len=100000") {
		t.Fatalf("expected the full 100000-byte payload reassembled, got:\n%s", out)
	}
	if strings.Contains(out, "reads=1") {
		t.Fatalf("expected more than one read_bytes call to reassemble 100000 bytes (proves a single read cannot), got:\n%s", out)
	}
}
