package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// UDP is what the connectionless protocols need — a BitTorrent tracker (BEP 15)
// or the DHT (BEP 5) talk to hundreds of hosts over one socket, so the peer's
// address has to travel with each packet instead of being fixed by a connect().
// This drives the whole surface at once: bind a server port, send a datagram to
// it, answer the sender using only the address recvfrom reported, and read the
// reply back on an ephemeral client socket.
func TestUDPRoundTrip(t *testing.T) {
	src := `func main() {
    srv := udp_socket(19731)
    if srv < 0 { println("bindfail")  exit(1) }
    go echo(srv)
    cli := udp_socket(0)
    socket_timeout(cli, 3000)
    udp_sendto(cli, "127.0.0.1", 19731, bytes("ping"))
    data, addr, port := udp_recvfrom(cli)
    println("reply=" + bytes_str(data))
    println("addr=" + addr)
    println("hasport=" + str(port > 0))
    close(cli)  close(srv)
}`
	echo := `func echo(srv) { d, a, p := udp_recvfrom(srv)  udp_sendto(srv, a, p, bytes(bytes_str(d) + "-pong")) }`
	out := runNative(t, src, echo)
	for _, want := range []string{"reply=ping-pong", "addr=127.0.0.1", "hasport=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("udp round-trip output = %q, want it to contain %q", out, want)
		}
	}
}

// A datagram is NUL-safe and length-driven: a binary tracker response is full of
// zero bytes, so a payload that truncated at the first NUL (as a C string would)
// would silently lose the rest of the packet.
func TestUDPCarriesNULBytes(t *testing.T) {
	src := `func main() {
    srv := udp_socket(19732)
    if srv < 0 { println("bindfail")  exit(1) }
    go relay(srv)
    cli := udp_socket(0)
    socket_timeout(cli, 3000)
    udp_sendto(cli, "127.0.0.1", 19732, from_hex("00ff00ff0041"))
    data, _, _ := udp_recvfrom(cli)
    println("hex=" + to_hex(data))
    println("len=" + str(len(data)))
    close(cli)  close(srv)
}`
	relay := `func relay(srv) { d, a, p := udp_recvfrom(srv)  udp_sendto(srv, a, p, d) }`
	out := runNative(t, src, relay)
	for _, want := range []string{"hex=00ff00ff0041", "len=6"} {
		if !strings.Contains(out, want) {
			t.Fatalf("udp NUL-safety output = %q, want it to contain %q", out, want)
		}
	}
}

// A timeout must be distinguishable from a datagram that legitimately carried no
// payload. Nothing arriving reports port 0; a real (empty) datagram reports the
// sender's real port, so `port != 0` is the check a caller can rely on.
func TestUDPRecvTimeoutReportsZeroPort(t *testing.T) {
	src := `func main() {
    fd := udp_socket(19733)
    if fd < 0 { println("bindfail")  exit(1) }
    socket_timeout(fd, 200)
    data, addr, port := udp_recvfrom(fd)
    println("len=" + str(len(data)))
    println("port=" + str(port))
    println("addr=[" + addr + "]")
    close(fd)
}`
	out := runNative(t, src)
	for _, want := range []string{"len=0", "port=0", "addr=[]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("udp timeout output = %q, want it to contain %q", out, want)
		}
	}
}

// write_file_at exists so a file can be filled OUT OF ORDER. Pieces of a torrent
// arrive in whatever order peers supply them; without a positional write the
// only options are buffering the whole file in RAM or a piece-file store.
// Writing the tail first must create the file and extend it, and the later
// head-write must not truncate what is already there.
func TestWriteFileAtOutOfOrder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pieces.bin")
	src := `func main() {
    println("tail=" + str(write_file_at("` + p + `", 8, bytes("TAIL"))))
    println("head=" + str(write_file_at("` + p + `", 0, bytes("HEAD"))))
    println("mid=" + str(write_file_at("` + p + `", 4, bytes("MIDL"))))
    println("out=" + read_file("` + p + `"))
}`
	out := runNative(t, src)
	for _, want := range []string{"tail=4", "head=4", "mid=4", "out=HEADMIDLTAIL"} {
		if !strings.Contains(out, want) {
			t.Fatalf("write_file_at output = %q, want it to contain %q", out, want)
		}
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "HEADMIDLTAIL" {
		t.Fatalf("on-disk content = %q err=%v", b, err)
	}
}

// Writing past the end leaves a hole, so the file's size reflects the highest
// offset written even though the gap was never written to. A downloader relies
// on this to allocate nothing up front.
func TestWriteFileAtLeavesHole(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sparse.bin")
	src := `func main() {
    write_file_at("` + p + `", 1000, bytes("end"))
    println("size=" + str(file_size("` + p + `")))
}`
	if out := runNative(t, src); !strings.Contains(out, "size=1003") {
		t.Fatalf("sparse write size = %q, want size=1003", out)
	}
}

// A negative offset is a caller bug, not something to translate into a wild
// seek: it reports -1 and writes nothing.
func TestWriteFileAtRejectsNegativeOffset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "neg.bin")
	src := `func main() {
    println("r=" + str(write_file_at("` + p + `", -1, bytes("x"))))
}`
	if out := runNative(t, src); !strings.Contains(out, "r=-1") {
		t.Fatalf("negative offset = %q, want r=-1", out)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("negative offset created the file (err=%v), want no file", err)
	}
}
