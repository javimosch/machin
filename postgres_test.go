package main

import (
	"os"
	"strings"
	"testing"
)

// Integration test for the pure-MFL Postgres client (framework/postgres.src):
// SCRAM-SHA-256 auth + simple query, against a LIVE Postgres. It is gated behind
// MACHIN_PG_TEST so the normal suite (and CI, which has no database) skips it and
// never blocks on dial. Run locally with a server up:
//
//   docker run -d --name machin-pg -e POSTGRES_PASSWORD=machin -e POSTGRES_DB=machindb -p 5432:5432 postgres:16
//   MACHIN_PG_TEST=1 go test -run TestPostgres -v
//
// Override creds via MACHIN_PG_{HOST,PORT,USER,PASS,DB} (defaults match the line above).
func TestPostgresScramQuery(t *testing.T) {
	if os.Getenv("MACHIN_PG_TEST") == "" {
		t.Skip("set MACHIN_PG_TEST=1 (and run a Postgres) to exercise the wire client")
	}
	getenv := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	host := getenv("MACHIN_PG_HOST", "127.0.0.1")
	port := getenv("MACHIN_PG_PORT", "5432")
	user := getenv("MACHIN_PG_USER", "postgres")
	pass := getenv("MACHIN_PG_PASS", "machin")
	db := getenv("MACHIN_PG_DB", "machindb")

	data, err := os.ReadFile("framework/postgres.src")
	if err != nil {
		t.Skip("framework/postgres.src not found")
	}
	// self-contained: a TEMP table lives for the session, so the test owns its data.
	app := `
type Row struct { id int  name string  active bool }
func main() {
    pg_connect("` + host + `", ` + port + `, "` + user + `", "` + db + `", "` + pass + `")
    pg_query("CREATE TEMP TABLE t (id int, name text, active bool)")
    // parameterized INSERT (extended protocol): a value with a quote, bound as data
    pg_exec("INSERT INTO t VALUES ($1, $2, $3)", []string{"1", "Ada", "true"})
    pg_exec("INSERT INTO t VALUES ($1, $2, $3)", []string{"2", "O'Brien", "false"})
    rows := pg_query("SELECT id, name, active FROM t ORDER BY id")
    println("rows=" + rows)
    rs := parse(rows, []Row{})
    println("n=" + str(len(rs)))
    println("r0=" + str(rs[0].id) + ":" + rs[0].name)
    println("r1active=" + str(rs[1].active))
    // parameterized SELECT + an injection attempt that must stay inert
    hit := pg_exec("SELECT id, name, active FROM t WHERE name = $1", []string{"Ada"})
    println("hit=" + hit)
    inj := pg_exec("SELECT id FROM t WHERE name = $1", []string{"x'; DROP TABLE t; --"})
    println("inj=" + inj)
    println("survived=" + pg_query("SELECT count(*) AS n FROM t"))
    pg_disconnect()
}`
	prog, perr := progFromSrcErr(string(data) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		`"id":1,"name":"Ada","active":true`, // numeric/bool unquoted, string quoted
		`"name":"O'Brien"`,                  // a quote round-trips through the wire + JSON
		"n=2",
		"r0=1:Ada",        // parse([]Row{}) decoded the typed fields
		"r1active=false",  // bool column decoded
		`hit=[{"id":1,"name":"Ada","active":true}]`, // pg_exec bound $1
		"inj=[]",          // injection param matched nothing...
		`survived=[{"n":2}]`, // ...and the table is intact (DROP did not run)
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
