package main

import (
	"os"
	"strings"
	"testing"
)

// Connection pooling: a concurrent server handles each request in its own goroutine,
// so the datastore clients can't share one connection. The pool is an async channel
// of authenticated fds (a semaphore) and each acquired connection has its own read
// buffer — so N goroutines over K connections never interleave. These gated tests
// hammer the pool and assert every worker gets ITS OWN correct result back.

// Postgres pool: 30 goroutines, 4 connections; each selects its own id back.
func TestPostgresPoolConcurrent(t *testing.T) {
	if os.Getenv("MACHIN_PG_TEST") == "" {
		t.Skip("set MACHIN_PG_TEST=1 (and run a Postgres) to exercise the pool")
	}
	host := "127.0.0.1"
	if v := os.Getenv("MACHIN_PG_HOST"); v != "" {
		host = v
	}
	data, err := os.ReadFile("framework/postgres.src")
	if err != nil {
		t.Skip("framework/postgres.src not found")
	}
	app := `
type R struct { v string }
var done = make(chan int)
func worker(id) {
    c := pg_acquire()
    rows := pgx(c, "SELECT $1 AS v", []string{str(id)})
    pg_release(c)
    rs := parse(rows, []R{})
    ok := 0
    if len(rs) == 1 { if rs[0].v == str(id) { ok = 1 } }
    done <- ok
}
func main() {
    pg_pool_init(4, "` + host + `", 5432, "postgres", "machindb", "machin")
    n := 30
    i := 0
    while i < n { go worker(i)  i = i + 1 }
    good := 0
    j := 0
    while j < n { good = good + <-done  j = j + 1 }
    println("ok=" + str(good) + "/" + str(n))
}`
	prog, perr := progFromSrcErr(string(data) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "ok=30/30") {
		t.Fatalf("pool corrupted a result under concurrency; got:\n%s", out)
	}
}

// Mongo pool: 30 goroutines, 4 connections; each inserts + filtered-finds its own doc.
func TestMongoPoolConcurrent(t *testing.T) {
	if os.Getenv("MACHIN_MONGO_TEST") == "" {
		t.Skip("set MACHIN_MONGO_TEST=1 (and run a MongoDB) to exercise the pool")
	}
	host := "127.0.0.1"
	if v := os.Getenv("MACHIN_MONGO_HOST"); v != "" {
		host = v
	}
	bson, err := os.ReadFile("framework/bson.src")
	if err != nil {
		t.Skip("framework/bson.src not found")
	}
	mongo, err := os.ReadFile("framework/mongo.src")
	if err != nil {
		t.Skip("framework/mongo.src not found")
	}
	app := `
type R struct { i int }
var done = make(chan int)
func worker(id) {
    c := mongo_acquire()
    mins(c, "mpooltest", "items", bson_finish(bson_i32(bson_new(), "i", id)))
    arr := mfind(c, "mpooltest", "items", bson_finish(bson_i32(bson_new(), "i", id)))
    mongo_release(c)
    rs := parse(arr, []R{})
    ok := 0
    if len(rs) == 1 { if rs[0].i == id { ok = 1 } }
    done <- ok
}
func main() {
    mongo_connect("` + host + `", 27017)  mongo_drop("mpooltest", "items")  mongo_close()
    mongo_pool_init(4, "` + host + `", 27017, "", "")
    n := 30
    i := 0
    while i < n { go worker(i)  i = i + 1 }
    good := 0
    j := 0
    while j < n { good = good + <-done  j = j + 1 }
    println("ok=" + str(good) + "/" + str(n))
}`
	out, err := RunCaptured(progFromSrcMust(t, string(bson)+string(mongo)+app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "ok=30/30") {
		t.Fatalf("pool corrupted a result under concurrency; got:\n%s", out)
	}
}

// Redis pool: 30 goroutines, 4 connections; each set/get its own key.
func TestRedisPoolConcurrent(t *testing.T) {
	if os.Getenv("MACHIN_REDIS_TEST") == "" {
		t.Skip("set MACHIN_REDIS_TEST=1 (and run a Redis) to exercise the pool")
	}
	host := "127.0.0.1"
	if v := os.Getenv("MACHIN_REDIS_HOST"); v != "" {
		host = v
	}
	data, err := os.ReadFile("framework/redis.src")
	if err != nil {
		t.Skip("framework/redis.src not found")
	}
	app := `
var done = make(chan int)
func worker(id) {
    c := redis_acquire()
    rset(c, "p:" + str(id), "v" + str(id))
    got, _ := rget(c, "p:" + str(id))
    redis_release(c)
    ok := 0
    if got == "v" + str(id) { ok = 1 }
    done <- ok
}
func main() {
    redis_pool_init(4, "` + host + `", 6379)
    n := 30
    i := 0
    while i < n { go worker(i)  i = i + 1 }
    good := 0
    j := 0
    while j < n { good = good + <-done  j = j + 1 }
    println("ok=" + str(good) + "/" + str(n))
}`
	prog, perr := progFromSrcErr(string(data) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "ok=30/30") {
		t.Fatalf("pool corrupted a result under concurrency; got:\n%s", out)
	}
}

// MySQL pool: 30 goroutines, 4 connections; each inserts + finds its own row.
func TestMySQLPoolConcurrent(t *testing.T) {
	if os.Getenv("MACHIN_MYSQL_TEST") == "" {
		t.Skip("set MACHIN_MYSQL_TEST=1 (and run a MariaDB/MySQL) to exercise the pool")
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
type R struct { i int }
var done = make(chan int)
func worker(id) {
    c := mysql_acquire()
    myx(c, "INSERT INTO pooltest (i) VALUES (" + str(id) + ")")
    rs := parse(myq(c, "SELECT i FROM pooltest WHERE i = " + str(id)), []R{})
    mysql_release(c)
    ok := 0
    if len(rs) == 1 { if rs[0].i == id { ok = 1 } }
    done <- ok
}
func main() {
    mysql_connect("` + host + `", 3307, "root", "secret", "cms")
    mysql_exec("DROP TABLE IF EXISTS pooltest")  mysql_exec("CREATE TABLE pooltest (i INT)")  mysql_close()
    mysql_pool_init(4, "` + host + `", 3307, "root", "secret", "cms")
    n := 30
    i := 0
    while i < n { go worker(i)  i = i + 1 }
    good := 0
    j := 0
    while j < n { good = good + <-done  j = j + 1 }
    println("ok=" + str(good) + "/" + str(n))
}`
	out, err := RunCaptured(progFromSrcMust(t, string(data)+app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "ok=30/30") {
		t.Fatalf("mysql pool corrupted a result; got:\n%s", out)
	}
}
