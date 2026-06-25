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
