package main

import (
	"os"
	"strings"
	"testing"
)

func redisProg(t *testing.T, app string) *Program {
	t.Helper()
	data, err := os.ReadFile("framework/redis.src")
	if err != nil {
		t.Skip("framework/redis.src not found")
	}
	prog, perr := progFromSrcErr(string(data) + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// RESP send + reply parsing across all five reply types, against an in-process mock
// that hands back a fixed reply per command (the client is synchronous: one command,
// one reply). CI-runnable — no Redis needed.
func TestRedisProtocol(t *testing.T) {
	app := `
func mock(conn, replies) {
    i := 0
    while i < len(replies) {
        req := read(conn)
        if req == "" { return }
        write(conn, replies[i])
        i = i + 1
    }
}
func serve_mock(port, replies) {
    srv := listen(port)
    conn := accept(srv)
    mock(conn, replies)
    close(conn)
}
func main() {
    replies := []string{"+OK\r\n", "$5\r\nhello\r\n", "$-1\r\n", ":7\r\n", "*2\r\n$1\r\na\r\n$3\r\nbbb\r\n"}
    go serve_mock(48310, replies)
    sleep(80)
    redis_connect("127.0.0.1", 48310)
    println("set=" + str(redis_set("k", "v")))           // +OK -> ok 1
    g, ok := redis_get("k")
    println("get=" + g + ":" + str(ok))                  // $5 hello -> "hello":1
    _, miss := redis_get("none")
    println("miss=" + str(miss))                         // $-1 -> ok 0
    println("incr=" + str(redis_incr("c")))              // :7 -> 7
    arr, _ := redis_lrange("l", 0, -1)
    println("arr=" + arr)                                // *2 -> JSON array
    items := parse(arr, []string{})
    println("n=" + str(len(items)) + " e1=" + items[1])
    redis_close()
}`
	out, err := RunCaptured(redisProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"set=1",           // simple-string reply -> ok
		"get=hello:1",     // bulk string
		"miss=0",          // $-1 nil -> ok 0
		"incr=7",          // integer reply
		`arr=["a","bbb"]`, // array -> JSON, composes with parse
		"n=2 e1=bbb",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// Integration test against a LIVE Redis (gated, like the Postgres one). Run with:
//
//	docker run -d --name machin-redis -p 6379:6379 redis:7
//	MACHIN_REDIS_TEST=1 go test -run TestRedisLive -v
func TestRedisLive(t *testing.T) {
	if os.Getenv("MACHIN_REDIS_TEST") == "" {
		t.Skip("set MACHIN_REDIS_TEST=1 (and run a Redis) to exercise the live client")
	}
	host := "127.0.0.1"
	if v := os.Getenv("MACHIN_REDIS_HOST"); v != "" {
		host = v
	}
	app := `
func main() {
    redis_connect("` + host + `", 6379)
    fr, fk := redis_cmd([]string{"FLUSHALL"})
    println("set=" + str(redis_set("greeting", "hi there")))
    v, ok := redis_get("greeting")
    println("get=" + v + ":" + str(ok))
    _, miss := redis_get("nope")
    println("miss=" + str(miss))
    println("incr=" + str(redis_incr("n")) + str(redis_incr("n")))
    println("setex=" + str(redis_setex("s", 60, "u1")) + " ex=" + str(redis_exists("s")))
    redis_rpush("q", "a")  redis_rpush("q", "b")
    arr, _ := redis_lrange("q", 0, -1)
    println("arr=" + arr)
    redis_close()
}`
	out, err := RunCaptured(redisProg(t, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"set=1", "get=hi there:1", "miss=0", "incr=12",
		"setex=1 ex=1", `arr=["a","b"]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
