package main

import (
	"os"
	"strings"
	"testing"
)

// The BSON codec (framework/bson.src) round-trips a built document back to JSON:
// strings, int32/int64, bool, null, embedded docs, and arrays. CI-runnable (pure).
func TestBsonRoundTrip(t *testing.T) {
	data, err := os.ReadFile("framework/bson.src")
	if err != nil {
		t.Skip("framework/bson.src not found")
	}
	app := `
func main() {
    d := bson_new()
    d = bson_str(d, "name", "Ada \"Lovelace\"")   // value with a quote (JSON-escaped on decode)
    d = bson_i32(d, "age", 36)
    d = bson_i64(d, "big", 9000000000)
    d = bson_bool(d, "active", 1)
    d = bson_null(d, "note")
    println("flat=" + bson_to_json(bson_finish(d)))
    sub := bson_finish(bson_str(bson_new(), "x", "hi"))
    a := bson_i32(bson_i32(bson_new(), "0", 10), "1", 20)
    top := bson_subarr(bson_subdoc(bson_i32(bson_new(), "n", 1), "sub", sub), "arr", bson_finish(a))
    println("nested=" + bson_to_json(bson_finish(top)))
}`
	out, err := RunCaptured(progFromSrcMust(t, string(data)+app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		`flat={"name":"Ada \"Lovelace\"","age":36,"big":9000000000,"active":true,"note":null}`,
		`nested={"n":1,"sub":{"x":"hi"},"arr":[10,20]}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// Live MongoDB client (gated). Run with:
//   docker run -d --name machin-mongo -p 27017:27017 mongo:7
//   MACHIN_MONGO_TEST=1 go test -run TestMongo -v
func TestMongoClient(t *testing.T) {
	if os.Getenv("MACHIN_MONGO_TEST") == "" {
		t.Skip("set MACHIN_MONGO_TEST=1 (and run a MongoDB) to exercise the wire client")
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
type Person struct { name string  age int }
func main() {
    mongo_connect("` + host + `", 27017)
    mongo_drop("machintest", "people")
    mongo_insert_one("machintest", "people", bson_finish(bson_str(bson_i32(bson_new(), "age", 36), "name", "Ada")))
    mongo_insert_one("machintest", "people", bson_finish(bson_str(bson_i32(bson_new(), "age", 41), "name", "O'Brien")))
    println("count=" + str(mongo_count("machintest", "people")))
    docs := mongo_find_all("machintest", "people")
    ps := parse(docs, []Person{})
    println("n=" + str(len(ps)) + " a0=" + str(ps[0].age) + " name1=" + ps[1].name)
    mongo_close()
}`
	out, err := RunCaptured(progFromSrcMust(t, string(bson)+string(mongo)+app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"count=2", "n=2 a0=36 name1=O'Brien"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func progFromSrcMust(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := progFromSrcErr(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}
