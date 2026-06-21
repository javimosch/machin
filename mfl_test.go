package main

import (
	"encoding/base64"
	"testing"
)

// runNative parses readable function sources, round-trips them through base64
// (the real machine-first path), compiles to native via cc, runs, and returns
// stdout.
func runNative(t *testing.T, funcs ...string) string {
	t.Helper()
	var fns []*FuncDecl
	for _, f := range funcs {
		enc := base64.StdEncoding.EncodeToString([]byte(normalize(f)))
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("b64: %v", err)
		}
		fn, err := ParseFunc(string(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		fns = append(fns, fn)
	}
	out, err := RunCaptured(&Program{Funcs: fns})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

func TestArithmetic(t *testing.T) {
	if got := runNative(t, `func main() { println(2 + 3 * 4) }`); got != "14\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRecursionAndLoop(t *testing.T) {
	got := runNative(t,
		`func fib(n) { if n < 2 { return n } return fib(n-1) + fib(n-2) }`,
		`func main() { println(fib(10)) }`)
	if got != "55\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringsAndBools(t *testing.T) {
	if got := runNative(t, `func main() { println("a" + "b", 1 == 1, !false) }`); got != "ab true true\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFloatInference(t *testing.T) {
	// k starts int but unifies to float via 2.0 * k; division is float.
	got := runNative(t, `func main() { k := 0 println(7 / 2, 2.0 * k + 7.0 / 2.0) }`)
	if got != "3 3.5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringBuilding(t *testing.T) {
	got := runNative(t,
		`func bin(n) { if n < 2 { return str(n) } return bin(n/2) + str(n%2) }`,
		`func main() { println(bin(10)) }`)
	if got != "1010\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSlices(t *testing.T) {
	got := runNative(t,
		`func main() { xs := []int{1, 2, 3} xs = append(xs, 4) xs[0] = 10 s := 0 i := 0 for i < len(xs) { s = s + xs[i] i = i + 1 } println(s, len(xs)) }`)
	if got != "19 4\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSliceParamInference(t *testing.T) {
	got := runNative(t,
		`func first(xs) { return xs[0] }`,
		`func main() { println(first([]string{"a", "b"})) }`)
	if got != "a\n" {
		t.Fatalf("got %q", got)
	}
}

func TestGoroutine(t *testing.T) {
	got := runNative(t,
		`func w() { println("hi") }`,
		`func main() { go w() sleep(50) println("done") }`)
	if got != "hi\ndone\n" {
		t.Fatalf("got %q", got)
	}
}

// runProg runs a whole program (struct types + functions) through the native path.
func runProg(t *testing.T, srcs ...string) string {
	t.Helper()
	var decls []string
	for _, s := range srcs {
		decls = append(decls, normalize(s))
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

func TestStructFieldsAndAssign(t *testing.T) {
	got := runProg(t,
		`type P struct { x int  y int }`,
		`func main() { p := P{x: 3, y: 4} p.x = 10 println(p.x + p.y) }`)
	if got != "14\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStructParamReturnAndSlice(t *testing.T) {
	got := runProg(t,
		`type P struct { x int  y int }`,
		`func mk(a, b) { return P{x: a, y: b} }`,
		`func main() { ps := []P{} ps = append(ps, mk(1, 2)) ps = append(ps, mk(3, 4)) s := 0 i := 0 for i < len(ps) { s = s + ps[i].x + ps[i].y i = i + 1 } println(s, len(ps)) }`)
	if got != "10 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStructFieldTypeMismatch(t *testing.T) {
	prog, err := ParseProgram([]string{
		normalize(`type P struct { x int }`),
		normalize(`func main() { p := P{x: "no"} println(p.x) }`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(prog); err == nil {
		t.Fatal("expected field type mismatch error")
	}
}

func TestMapStringKeys(t *testing.T) {
	got := runProg(t,
		`func main() { m := make(map[string]int) m["a"] = 1 m["a"] = m["a"] + 5 println(m["a"], m["missing"], has(m, "a"), has(m, "z"), len(m)) }`)
	if got != "6 0 true false 1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMapIntKeysAndDelete(t *testing.T) {
	got := runProg(t,
		`func main() { m := make(map[int]string) m[1] = "one" m[2] = "two" delete(m, 1) println(m[1], m[2], len(m), has(m, 1)) }`)
	if got != " two 1 false\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMapKeysSliceSum(t *testing.T) {
	got := runProg(t,
		`func main() { m := make(map[int]int) m[3] = 30 m[7] = 70 ks := keys(m) s := 0 i := 0 for i < len(ks) { s = s + m[ks[i]] i = i + 1 } println(s) }`)
	if got != "100\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJSONScalarsAndSlice(t *testing.T) {
	got := runProg(t, `func main() { println(json(42), json(true), json([]int{1, 2, 3})) }`)
	if got != "42 true [1,2,3]\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJSONStringEscape(t *testing.T) {
	// MFL string  he said "hi"  ->  JSON  "he said \"hi\""
	got := runProg(t, `func main() { println(json("he said \"hi\"")) }`)
	if got != "\"he said \\\"hi\\\"\"\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJSONStructAndMap(t *testing.T) {
	got := runProg(t,
		`type P struct { x int  y string }`,
		`func main() { println(json(P{x: 1, y: "ab"})) m := make(map[string]int) m["k"] = 7 println(json(m)) }`)
	if got != "{\"x\":1,\"y\":\"ab\"}\n{\"k\":7}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJSONParseStructRoundTrip(t *testing.T) {
	got := runProg(t,
		`type P struct { x int  y string  ok bool }`,
		`func main() { p := parse(json(P{x: 5, y: "hi", ok: true}), P{}) println(p.x, p.y, p.ok) }`)
	if got != "5 hi true\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJSONParseStructToleratesOrderAndExtras(t *testing.T) {
	got := runProg(t,
		`type P struct { a int  b int }`,
		`func main() { p := parse("{\"b\":2,\"extra\":9,\"a\":1}", P{}) println(p.a, p.b) }`)
	if got != "1 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJSONParseSliceAndMap(t *testing.T) {
	got := runProg(t,
		`func main() { xs := parse("[10, 20, 30]", []int{}) m := parse("{\"k\":9}", make(map[string]int)) println(len(xs), xs[2], m["k"]) }`)
	if got != "3 30 9\n" {
		t.Fatalf("got %q", got)
	}
}

func TestHTTPBody(t *testing.T) {
	got := runProg(t,
		`func main() { println(http_body("POST / HTTP/1.1\r\nHost: x\r\n\r\nthe-body")) }`)
	if got != "the-body\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringOps(t *testing.T) {
	got := runProg(t,
		`func main() { s := "Hello, World" println(to_upper(s), substr(s, 7, 12), index(s, "World"), contains(s, "lo"), has_prefix(s, "He"), has_suffix(s, "ld")) }`)
	if got != "HELLO, WORLD World 7 true true true\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringSplitJoinReplaceTrim(t *testing.T) {
	got := runProg(t,
		`func main() { p := split("a,b,c", ",") println(len(p), p[1], join(p, "-"), replace("a.b.c", ".", "/"), trim("  hi  ")) }`)
	if got != "3 b a-b-c a/b/c hi\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringRouteParse(t *testing.T) {
	// the request-routing pattern: split a request line, extract a path segment
	got := runProg(t,
		`func main() { f := split("GET /users/42 HTTP/1.1", " ") seg := split(f[1], "/") println(f[0], seg[1], seg[2]) }`)
	if got != "GET users 42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestClosureCapture(t *testing.T) {
	got := runProg(t,
		`func adder(n) { return func(x) { return x + n } }`,
		`func main() { inc := adder(1) add10 := adder(10) println(inc(5), add10(5)) }`)
	if got != "6 15\n" {
		t.Fatalf("got %q", got)
	}
}

func TestClosureHigherOrder(t *testing.T) {
	got := runProg(t,
		`func apply(f, x) { return f(x) }`,
		`func main() { factor := 3 println(apply(func(x) { return x * factor }, 7)) }`)
	if got != "21\n" {
		t.Fatalf("got %q", got)
	}
}

func TestClosureIIFE(t *testing.T) {
	got := runProg(t, `func main() { println(func(a, b) { return a * b }(6, 7)) }`)
	if got != "42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiReturnDestructure(t *testing.T) {
	got := runProg(t,
		`func divmod(a, b) { return a / b, a % b }`,
		`func main() { q, r := divmod(17, 5) println(q, r) }`)
	if got != "3 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiReturnCommaOk(t *testing.T) {
	got := runProg(t,
		`func lookup(m, k) { return m[k], has(m, k) }`,
		`func main() { m := make(map[string]int) m["x"] = 7 v, ok := lookup(m, "x") w, no := lookup(m, "y") println(v, ok, w, no) }`)
	if got != "7 true 0 false\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiReturnIgnoreAndSwap(t *testing.T) {
	got := runProg(t,
		`func pair() { return 10, 20 }`,
		`func main() { _, b := pair() x := 1 y := 2 x, y = y, x println(b, x, y) }`)
	if got != "20 2 1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiReturnInExprIsError(t *testing.T) {
	prog, err := ParseProgram([]string{
		normalize(`func two() { return 1, 2 }`),
		normalize(`func main() { x := two() println(x) }`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(prog); err == nil {
		t.Fatal("expected error using a 2-value function in single-value context")
	}
}

func TestRangeSlice(t *testing.T) {
	got := runProg(t, `func main() { xs := []int{2, 4, 6} s := 0 for i, v := range xs { s = s + i * v } println(s) }`)
	if got != "16\n" { // 0*2 + 1*4 + 2*6
		t.Fatalf("got %q", got)
	}
}

func TestRangeStringChars(t *testing.T) {
	got := runProg(t, `func main() { out := "" for i, c := range "xyz" { out = out + c } println(out) }`)
	if got != "xyz\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRangeMapValues(t *testing.T) {
	// iteration order is unspecified, so sum (order-independent)
	got := runProg(t, `func main() { m := make(map[string]int) m["a"] = 1 m["b"] = 2 m["c"] = 3 s := 0 for k, v := range m { s = s + v } println(s) }`)
	if got != "6\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRangeKeyOnly(t *testing.T) {
	got := runProg(t, `func main() { xs := []int{9, 9, 9, 9} c := 0 for i := range xs { c = i } println(c) }`)
	if got != "3\n" {
		t.Fatalf("got %q", got)
	}
}

func TestChannelSendRecv(t *testing.T) {
	got := runProg(t,
		`func send(c) { c <- 42 }`,
		`func main() { ch := make(chan int) go send(ch) println(<-ch) }`)
	if got != "42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestChannelFanIn(t *testing.T) {
	// three values produced on a goroutine, summed by main via the channel
	got := runProg(t,
		`func prod(c) { i := 1 for i <= 3 { c <- i * 10 i = i + 1 } }`,
		`func main() { c := make(chan int) go prod(c) s := 0 i := 0 for i < 3 { s = s + <-c i = i + 1 } println(s) }`)
	if got != "60\n" {
		t.Fatalf("got %q", got)
	}
}

func TestChannelElemInference(t *testing.T) {
	// the channel element type is inferred from the send, used by recv
	got := runProg(t,
		`func main() { c := make(chan string) go reply(c) println(<-c) }`,
		`func reply(c) { c <- "pong" }`)
	if got != "pong\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTypeMismatch(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { x := 1 x = "s" }`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(&Program{Funcs: []*FuncDecl{fn}}); err == nil {
		t.Fatal("expected type mismatch error")
	}
}

func TestSplitFunctions(t *testing.T) {
	fns, err := splitFunctions("func a() { return 1 }\n\nfunc b() { return 2 }\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 2 {
		t.Fatalf("expected 2 funcs, got %d", len(fns))
	}
}
