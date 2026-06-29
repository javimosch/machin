package main

import (
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
