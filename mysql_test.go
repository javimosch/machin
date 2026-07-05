package main

import (
	"os"
	"strings"
	"testing"
)

// The MySQL/MariaDB client (framework/mysql.src): mysql_native_password auth (SHA-1)
//   - text-protocol queries returning typed JSON rows. Gated — needs a live server:
//     docker run -d --name m -e MARIADB_ROOT_PASSWORD=secret -e MARIADB_DATABASE=cms -p 3307:3306 mariadb:11
//     MACHIN_MYSQL_TEST=1 go test -run TestMySQL -v
func TestMySQLClient(t *testing.T) {
	if os.Getenv("MACHIN_MYSQL_TEST") == "" {
		t.Skip("set MACHIN_MYSQL_TEST=1 (and run a MariaDB/MySQL) to exercise the wire client")
	}
	host := "127.0.0.1"
	if v := os.Getenv("MACHIN_MYSQL_HOST"); v != "" {
		host = v
	}
	data, err := os.ReadFile("framework/mysql.src")
	if err != nil {
		t.Skip("framework/mysql.src not found")
	}
	app := `
type Row struct { id int  name string  score float }
func main() {
    println("connect=" + str(mysql_connect("` + host + `", 3307, "root", "secret", "cms")))
    mysql_exec("CREATE TABLE IF NOT EXISTS t (id INT PRIMARY KEY AUTO_INCREMENT, name VARCHAR(64), score DOUBLE)")
    mysql_exec("DELETE FROM t")
    n := mysql_exec("INSERT INTO t (name, score) VALUES ('Ada', 3.5), ('" + mysql_escape("O'Brien") + "', 2.5)")
    println("inserted=" + str(n))
    rs := parse(mysql_query("SELECT id, name, score FROM t ORDER BY id"), []Row{})
    println("n=" + str(len(rs)) + " s0=" + str(rs[0].score) + " name1=" + rs[1].name)
    mysql_close()
}`
	out, err := RunCaptured(progFromSrcMust(t, string(data)+app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"connect=1", // mysql_native_password auth succeeded
		"inserted=2",
		"n=2 s0=3.5 name1=O'Brien", // typed (double) decode + an escaped quote round-trip
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
