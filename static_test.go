package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A `--static` (BuildBinaryStatic) build of a SQLite-using program must compile the
// bundled amalgamation in — so the binary does NOT depend on libsqlite3 — and still
// work. This is the FROM-scratch deploy story for the common REST+SQLite shape.
// Uses the default cc (glibc -static), so it needs no musl on the test host.
func TestStaticBuildBundlesSqlite(t *testing.T) {
	bin, err := os.CreateTemp("", "mfl-static-sqlite-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())

	src := `func main() { db := sqlite_open(":memory:") sqlite_exec(db, "CREATE TABLE t(x int)") sqlite_exec(db, "INSERT INTO t VALUES(7)") println(sqlite_query(db, "SELECT x FROM t")) }`
	prog := &Program{Funcs: parseFuncs(t, src)}
	if err := BuildBinaryStatic(prog, bin.Name(), false, true); err != nil {
		t.Fatalf("static build of a SQLite program failed: %v", err)
	}

	// The whole point: no libsqlite3 dependency (it's compiled in from the amalgamation).
	if ldd, err := exec.Command("ldd", bin.Name()).CombinedOutput(); err == nil {
		if strings.Contains(string(ldd), "libsqlite3") {
			t.Fatalf("--static still links libsqlite3 dynamically:\n%s", ldd)
		}
	}

	out, err := exec.Command(bin.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("running the static binary failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"x":7`) {
		t.Fatalf("SQLite didn't work in the static binary; got: %s", out)
	}
}

// A `--static` TLS-using program gets a statically-linked OpenSSL plus an embedded
// CA root bundle compiled in (issue #283), so it can verify certificates with no
// external CA store — the FROM-scratch story extended from SQLite to TLS. Checked
// structurally (no dynamic libssl/libcrypto, the bundle bytes are really compiled
// in, not just linked with no effect) and behaviorally against a local self-signed
// TLS server: since that cert is untrusted by both the system store and the
// embedded bundle, http_get must reject it — proving verification is actually
// active. (The positive case — a globally-trusted cert accepted with zero files in
// a real `FROM scratch` container — was verified manually via Docker for issue
// #283; not automated here to avoid a live-internet test.)
func TestStaticBuildBundlesCACerts(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	bin, err := os.CreateTemp("", "mfl-static-tls-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())

	src := fmt.Sprintf(`func main() { status, body, err := http_get(%q)
	if len(err) > 0 { println("ERR:" + err)  return }
	println(str(status) + " " + body) }`, srv.URL)
	prog := &Program{Funcs: parseFuncs(t, src)}
	if err := BuildBinaryStatic(prog, bin.Name(), false, true); err != nil {
		t.Fatalf("static build of a TLS program failed: %v", err)
	}

	// Structural: no dynamic OpenSSL.
	if ldd, err := exec.Command("ldd", bin.Name()).CombinedOutput(); err == nil {
		if strings.Contains(string(ldd), "libssl") || strings.Contains(string(ldd), "libcrypto") {
			t.Fatalf("--static TLS build still links OpenSSL dynamically:\n%s", ldd)
		}
	}
	// The CA bundle must actually be compiled in, not just linked with no effect.
	data, err := os.ReadFile(bin.Name())
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "BEGIN CERTIFICATE"); n < 100 {
		t.Fatalf("expected the embedded CA bundle's certs in the binary, found %d BEGIN CERTIFICATE markers", n)
	}

	// Behavioral: a self-signed cert (trusted by neither the system store nor the
	// embedded bundle) must be REJECTED — proving verification is really active.
	out, err := exec.Command(bin.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("running the static TLS binary failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ERR:tls") {
		t.Fatalf("expected the self-signed local server to be rejected (ERR:tls), got: %s", out)
	}
}

// fakeDNSReply answers one DNS query datagram for the test names below:
// dns.test A->127.0.0.1 / AAAA->::1, nx.test->NXDOMAIN, anything else NODATA.
func fakeDNSReply(q []byte) []byte {
	if len(q) < 12 {
		return nil
	}
	off := 12
	var labels []string
	for {
		if off >= len(q) {
			return nil
		}
		l := int(q[off])
		off++
		if l == 0 {
			break
		}
		if l > 63 || off+l > len(q) {
			return nil
		}
		labels = append(labels, string(q[off:off+l]))
		off += l
	}
	if off+4 > len(q) {
		return nil
	}
	qtype := binary.BigEndian.Uint16(q[off:])
	qend := off + 4
	name := strings.Join(labels, ".")

	var rcode uint16
	var rdata []byte
	var rtype uint16
	if name == "nx.test" {
		rcode = 3 // NXDOMAIN
	} else if name == "dns.test" && (qtype == 1 || qtype == 28) {
		rtype = qtype
		if qtype == 1 {
			rdata = []byte{127, 0, 0, 1}
		} else {
			rdata = make([]byte, 16)
			rdata[15] = 1
		}
	}
	an := uint16(0)
	if rdata != nil {
		an = 1
	}

	resp := make([]byte, 0, qend+16+len(rdata))
	resp = append(resp, q[:2]...)                      // id
	resp = append(resp, 0x81, byte(0x80|rcode))       // QR|RD|RA + rcode
	resp = append(resp, 0, 1, 0, byte(an), 0, 0, 0, 0) // QD=1 AN NS=0 AR=0
	resp = append(resp, q[12:qend]...)                // question verbatim
	if an == 1 {
		resp = append(resp, 0xc0, 0x0c) // name -> offset 12
		resp = append(resp, byte(rtype>>8), byte(rtype))
		resp = append(resp, 0, 1)                   // class IN
		resp = append(resp, 0, 0, 0, 60)            // ttl
		resp = append(resp, byte(len(rdata)>>8), byte(len(rdata)))
		resp = append(resp, rdata...)
	}
	return resp
}

// A `--static` build must resolve names WITHOUT glibc's NSS: on a host whose
// glibc differs from the build's, getaddrinfo's dlopen()ed NSS modules die in
// the second libc's init (SIGFPE, #764). The emitted resolver does it with
// /etc/hosts + raw DNS instead — verified here end-to-end against a fake
// nameserver, with the config files relocated via MFL_HOSTS_FILE /
// MFL_RESOLV_CONF. The DNS leg needs to bind 127.0.0.1:53, so it is skipped
// where that needs privileges the test doesn't have.
func TestStaticBuildResolvesNamesWithoutNSS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	hosts := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hosts, []byte("127.0.0.1 testhost.local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolv := filepath.Join(dir, "resolv.conf")
	if err := os.WriteFile(resolv, []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Fake nameserver on 127.0.0.1:53 — only when the test may bind it.
	dnsAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:53")
	if err != nil {
		t.Fatal(err)
	}
	dns, err := net.ListenUDP("udp", dnsAddr)
	dnsOK := err == nil
	if dnsOK {
		defer dns.Close()
		go func() {
			buf := make([]byte, 4096)
			for {
				n, peer, err := dns.ReadFromUDP(buf)
				if err != nil {
					return
				}
				if resp := fakeDNSReply(buf[:n]); resp != nil {
					// first a forgery: right id, another question, claiming the
					// name does not exist. Accepting it would fail dns.test.
					forged := append([]byte(nil), resp...)
					if len(forged) > 13 {
						forged[13] ^= 1
						forged[3] = 0x83
						dns.WriteToUDP(forged, peer)
					}
					dns.WriteToUDP(resp, peer)
				}
			}
		}()
	}

	// Without a nameserver the DNS names would each burn the resolver's whole
	// timeout, so the program only exercises what can answer quickly.
	src := fmt.Sprintf(`func main() {
	st, body, err := http_request("GET", "http://testhost.local:%s/", []string{}, "")
	println("hosts: " + str(st) + " " + body + " " + err)
}`, port)
	if dnsOK {
		src = fmt.Sprintf(`func main() {
	st, body, err := http_request("GET", "http://testhost.local:%s/", []string{}, "")
	println("hosts: " + str(st) + " " + body + " " + err)
	st2, body2, err2 := http_request("GET", "http://dns.test:%s/", []string{}, "")
	println("dns: " + str(st2) + " " + body2 + " " + err2)
	st3, body3, err3 := http_request("GET", "http://nx.test:1/", []string{}, "")
	println("nxdomain: " + str(st3) + " " + err3)
}`, port, port)
	}
	prog := &Program{Funcs: parseFuncs(t, src)}

	bin, err := os.CreateTemp("", "mfl-static-resolv-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinaryStatic(prog, bin.Name(), false, true); err != nil {
		t.Fatalf("static build failed: %v", err)
	}

	cmd := exec.Command(bin.Name())
	cmd.Env = append(os.Environ(), "MFL_HOSTS_FILE="+hosts, "MFL_RESOLV_CONF="+resolv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running the static binary failed: %v\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "hosts: 200 ok ") {
		t.Fatalf("hosts-file resolution failed; got: %s", s)
	}
	if dnsOK {
		if !strings.Contains(s, "dns: 200 ok ") {
			t.Fatalf("DNS resolution failed; got: %s", s)
		}
		if !strings.Contains(s, "nxdomain: 0 dns") {
			t.Fatalf("NXDOMAIN not reported as dns; got: %s", s)
		}
	} else {
		t.Logf("127.0.0.1:53 not bindable; DNS leg skipped. Output:\n%s", s)
	}
}
