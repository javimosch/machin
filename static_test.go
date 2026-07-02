package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
