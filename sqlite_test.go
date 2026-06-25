package main

import (
	"strings"
	"testing"
)

// The SQLite builtins back the CRUD web demos: sqlite_open (":memory:" handle),
// sqlite_exec (statements, optional ?-bound []string params), and sqlite_query (a
// SELECT -> JSON array of rows, which composes with json_get). These run natively
// (build.go auto-links -lsqlite3 when mfl_sqlite_ appears in the emitted C).

// open + exec + query round-trip: a created/inserted row reads back, and the result
// set is a JSON array that json_get can index by row and field.
func TestSqliteRoundTrip(t *testing.T) {
	prog := progFromSrc(t, `
func jg(blob, path) (v) { v, _ = json_get(blob, path) }
func main() {
    db := sqlite_open(":memory:")
    if db == 0 { println("open-failed")  return }
    sqlite_exec(db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER)")
    sqlite_exec(db, "INSERT INTO users (name, active) VALUES ('Ada', 1)")
    sqlite_exec(db, "INSERT INTO users (name, active) VALUES ('Bo', 0)")
    rows := sqlite_query(db, "SELECT id, name, active FROM users ORDER BY id")
    println("rows=" + rows)
    println("name0=" + jg(rows, "[0].name"))
    println("active1=" + jg(rows, "[1].active"))
    println("count=" + jg(sqlite_query(db, "SELECT count(*) AS n FROM users"), "[0].n"))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "open-failed") {
		t.Fatal("sqlite_open(\":memory:\") returned 0")
	}
	for _, want := range []string{
		`name0="Ada"`,  // json_get returns the raw JSON token (a quoted string)
		`active1=0`,
		`count=2`,      // count(*) aliased to n -> both rows
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	// the query result is a JSON array of objects
	if !strings.Contains(out, `rows=[{`) {
		t.Fatalf("query should return a JSON array of row objects; got:\n%s", out)
	}
}

// Parameterized statements: the optional []string binds the ? placeholders, so
// user input never lands in the SQL text (injection-safe). A value containing a
// quote must round-trip verbatim — the proof it was bound, not interpolated.
func TestSqliteParamBinding(t *testing.T) {
	prog := progFromSrc(t, `
func jg(blob, path) (v) { v, _ = json_get(blob, path) }
func main() {
    db := sqlite_open(":memory:")
    sqlite_exec(db, "CREATE TABLE t (k TEXT, v TEXT)")
    sqlite_exec(db, "INSERT INTO t (k, v) VALUES (?, ?)", []string{"name", "O'Brien \"x\""})
    sqlite_exec(db, "INSERT INTO t (k, v) VALUES (?, ?)", []string{"drop", "'; DROP TABLE t; --"})
    // bound lookup returns the matching row
    hit := sqlite_query(db, "SELECT v FROM t WHERE k = ?", []string{"name"})
    println("hit=" + jg(hit, "[0].v"))
    // the injection attempt was stored as data, not executed: the table still has 2 rows
    println("rows=" + jg(sqlite_query(db, "SELECT count(*) AS n FROM t"), "[0].n"))
    // a non-matching param yields an empty result set
    miss := sqlite_query(db, "SELECT v FROM t WHERE k = ?", []string{"nope"})
    println("miss=[" + miss + "]")
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// the quote/double-quote value round-trips verbatim through the bind
	if !strings.Contains(out, `O'Brien`) {
		t.Fatalf("bound value with a quote should round-trip; got:\n%s", out)
	}
	// the DROP-TABLE string was stored, not run — table survives with both rows
	if !strings.Contains(out, "rows=2") {
		t.Fatalf("a parameter must not be executed as SQL (injection-safe); got:\n%s", out)
	}
	// empty result set is an empty JSON array
	if !strings.Contains(out, "miss=[[]]") {
		t.Fatalf("a non-matching query should return []; got:\n%s", out)
	}
}

// UPDATE/DELETE mutate the table and are observable on the next query — the write
// path the CRUD back-office relies on (each mutation reloads from the server).
func TestSqliteUpdateDelete(t *testing.T) {
	prog := progFromSrc(t, `
func jg(blob, path) (v) { v, _ = json_get(blob, path) }
func main() {
    db := sqlite_open(":memory:")
    sqlite_exec(db, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER)")
    sqlite_exec(db, "INSERT INTO users (name, active) VALUES (?, ?)", []string{"Ada", "1"})
    sqlite_exec(db, "INSERT INTO users (name, active) VALUES (?, ?)", []string{"Bo", "1"})
    sqlite_exec(db, "UPDATE users SET active = 0 WHERE name = ?", []string{"Bo"})
    println("bo_active=" + jg(sqlite_query(db, "SELECT active FROM users WHERE name='Bo'"), "[0].active"))
    println("n_active=" + jg(sqlite_query(db, "SELECT count(*) AS n FROM users WHERE active=1"), "[0].n"))
    sqlite_exec(db, "DELETE FROM users WHERE name = ?", []string{"Ada"})
    println("total=" + jg(sqlite_query(db, "SELECT count(*) AS n FROM users"), "[0].n"))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"bo_active=0", // UPDATE took effect
		"n_active=1",  // only Ada remains active
		"total=1",     // DELETE removed Ada
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
