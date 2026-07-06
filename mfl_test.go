package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// parse() must decode \uXXXX JSON escapes (UTF-8 encoding the code point) --
// #311: the old parser dropped the backslash and leaked the literal "uXXXX"
// text through, silently corrupting any JSON with an escaped non-ASCII char
// (common: LLM API responses escaping an em-dash, curly quote, etc).
func TestJSONParseUnicodeEscape(t *testing.T) {
	got := runProg(t,
		`type T struct { s string }`,
		`func main() { t := parse("{\"s\":\"a\\u000bb\\u2014d\"}", T{}) println(t.s) }`)
	want := "ab—d\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A surrogate pair (😀, an astral character outside the BMP) must
// combine into a single code point, not two separate replacement-ish values.
func TestJSONParseSurrogatePair(t *testing.T) {
	got := runProg(t,
		`type T struct { s string }`,
		`func main() { t := parse("{\"s\":\"emoji=\\uD83D\\uDE00 end\"}", T{}) println(t.s) }`)
	want := "emoji=\U0001F600 end\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A malformed \u escape (non-hex digits, or truncated at end of string) must
// degrade safely -- the literal "u" plus whatever follows, not a crash or an
// out-of-bounds read.
func TestJSONParseMalformedUnicodeEscape(t *testing.T) {
	got := runProg(t,
		`type T struct { s string  t string }`,
		`func main() { v := parse("{\"s\":\"bad=\\uZZZZend\",\"t\":\"short=\\u12\"}", T{}) println(v.s) println(v.t) }`)
	want := "bad=uZZZZend\nshort=u12\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
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

func TestJSONStructWithSliceField(t *testing.T) {
	// the shape the self-host /api/info endpoint serializes
	got := runProg(t,
		`type Info struct { name string  tags []string }`,
		`func main() { i := Info{name: "MFL", tags: []string{"a", "b"}} println(json(i)) }`)
	if got != "{\"name\":\"MFL\",\"tags\":[\"a\",\"b\"]}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestArenaAllocChurn(t *testing.T) {
	// heavy allocation churn with a live accumulator (arena alloc/realloc path)
	got := runProg(t,
		`func main() { acc := "" i := 0 for i < 5000 { junk := "x" + str(i) if i % 1000 == 0 { acc = acc + "|" } i = i + 1 } println(len(acc)) }`)
	if got != "5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestArenaGoroutineReclaim(t *testing.T) {
	// each goroutine builds a string in its own arena, sends the length (a value,
	// not a pointer), and its arena is freed on return — no corruption.
	got := runProg(t,
		`func work(c) { s := "" i := 0 for i < 100 { s = s + "x" i = i + 1 } c <- len(s) }`,
		`func main() { c := make(chan int) go work(c) go work(c) println(<-c + <-c) }`)
	if got != "200\n" {
		t.Fatalf("got %q", got)
	}
}

func TestGenericIdentity(t *testing.T) {
	// one source function specialized at int, string, and float
	got := runProg(t,
		`func id(x) { return x }`,
		`func main() { println(id(7), id("hi"), id(2.5)) }`)
	if got != "7 hi 2.5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestGenericContainer(t *testing.T) {
	got := runProg(t,
		`func third(xs) { return xs[2] }`,
		`func main() { println(third([]int{1, 2, 3}), third([]string{"a", "b", "c"})) }`)
	if got != "3 c\n" {
		t.Fatalf("got %q", got)
	}
}

func TestGenericHigherOrder(t *testing.T) {
	// a generic map over slices, used at two element types
	got := runProg(t,
		`func mapped(xs, f) { out := []int{} for _, v := range xs { out = append(out, f(v)) } return out }`,
		`func sumlen(xs) { s := 0 for _, v := range xs { s = s + v } return s }`,
		`func main() { a := mapped([]int{1, 2, 3}, func(x) { return x * 10 }) println(a[0], a[2]) }`)
	if got != "10 30\n" {
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

func TestMapOfClosuresRouter(t *testing.T) {
	// the map-based router pattern: handler closures stored in map[string]func,
	// dispatched by key, with a miss fallback
	got := runProg(t,
		`func reg(m, k, h) { m[k] = h }`,
		`func dispatch(m, k, x) (out) { if has(m, k) { h := m[k] out = h(x) } else { out = -1 } }`,
		`func main() { r := make(map[string]func) reg(r, "double", func(x) { return x * 2 }) reg(r, "square", func(x) { return x * x }) println(dispatch(r, "double", 5), dispatch(r, "square", 5), dispatch(r, "nope", 5), len(r)) }`)
	if got != "10 25 -1 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFrameworkDispatchPattern(t *testing.T) {
	// the machweb pattern: a handler closure returning a struct, dispatched
	// through a function that calls it (as serve -> handle -> handler does)
	got := runProg(t,
		`type Resp struct { code int  body string }`,
		`func dispatch(h, n) (r) { r = h(n) }`,
		`func main() { res := dispatch(func(x) { return Resp{code: 200, body: str(x * 2)} }, 21) println(res.code, res.body) }`)
	if got != "200 42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestVariadicCollect(t *testing.T) {
	got := runProg(t,
		`func sum(nums...) { t := 0 for _, n := range nums { t = t + n } return t }`,
		`func main() { println(sum(), sum(1, 2, 3), sum(10, 20, 30, 40)) }`)
	if got != "0 6 100\n" {
		t.Fatalf("got %q", got)
	}
}

func TestVariadicSpreadAndFixed(t *testing.T) {
	got := runProg(t,
		`func tail(first, rest...) { n := len(rest) return first + n }`,
		`func main() { xs := []int{7, 7, 7} println(tail(100, 1, 2), tail(100, xs...)) }`)
	if got != "102 103\n" {
		t.Fatalf("got %q", got)
	}
}

func TestVariadicGeneric(t *testing.T) {
	// one variadic function used at int and string
	got := runProg(t,
		`func cat(parts...) { s := "" for _, p := range parts { s = s + p } return s }`,
		`func count(parts...) { return len(parts) }`,
		`func main() { println(cat("a", "b", "c"), count(1, 2, 3, 4)) }`)
	if got != "abc 4\n" {
		t.Fatalf("got %q", got)
	}
}

func TestNamedReturnsBare(t *testing.T) {
	got := runProg(t,
		`func divmod(a, b) (q, r) { q = a / b r = a % b return }`,
		`func main() { q, r := divmod(17, 5) println(q, r) }`)
	if got != "3 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestNamedReturnsFallthrough(t *testing.T) {
	// no explicit return: the named value is returned at the end
	got := runProg(t,
		`func inc(n) (m) { m = n + 1 }`,
		`func main() { println(inc(41)) }`)
	if got != "42\n" {
		t.Fatalf("got %q", got)
	}
}

func TestNamedReturnsMixedExplicit(t *testing.T) {
	got := runProg(t,
		`func clamp(x, lo, hi) (y) { y = x if x < lo { return lo } if x > hi { return hi } return }`,
		`func main() { println(clamp(5, 0, 10), clamp(-3, 0, 10), clamp(99, 0, 10)) }`)
	if got != "5 0 10\n" {
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

// The OpenSSL/TLS runtime is emitted only when a program calls https_get/
// https_post; TLS-free programs must stay libc-only (no OpenSSL pulled in).
func TestHTTPSRuntimeGating(t *testing.T) {
	tls := &Program{Funcs: parseFuncs(t, `func main() { println(https_get("https://example.com")) }`)}
	c, err := CompileToC(tls, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_http_do") || !strings.Contains(c, "openssl/ssl.h") {
		t.Fatal("a program using https_get must emit the OpenSSL TLS runtime")
	}
	plain := &Program{Funcs: parseFuncs(t, `func main() { println("hi") }`)}
	c2, err := CompileToC(plain, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c2, "mfl_https_") || strings.Contains(c2, "openssl") {
		t.Fatal("a TLS-free program must stay libc-only (no OpenSSL emitted)")
	}
}

// http_get returns (status, body, err) via the v, err := idiom — the multi-return
// builtin path. Compiling must wire the destructure; using it as a single value
// must fail with a helpful message.
func TestHTTPGetMultiReturn(t *testing.T) {
	prog := &Program{Funcs: parseFuncs(t, `func main() { s, b, e := http_get("https://x") println(str(s) + b + e) }`)}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_http_result") || !strings.Contains(c, "mfl_http_get") {
		t.Fatal("http_get multi-return must emit the result struct + accessor")
	}
	// single-value misuse is a clear typecheck error, not a silent miscompile
	bad := &Program{Funcs: parseFuncs(t, `func main() { x := http_get("https://x") println(x) }`)}
	if _, err := CompileToC(bad, false); err == nil || !strings.Contains(err.Error(), "returns 3 values") {
		t.Fatalf("single-value http_get should error helpfully, got: %v", err)
	}
}

// http_request(method, url, []string headers, body) -> (status, body, err): an
// authenticated request with caller-supplied header lines. Same multi-return
// path as http_get; compiling must wire the accessor and gate the TLS runtime.
func TestHTTPRequest(t *testing.T) {
	src := `func main() { h := []string{"Authorization: Bearer x"}  s, b, e := http_request("POST", "https://x", h, "{}")  println(str(s) + b + e) }`
	c, err := CompileToC(&Program{Funcs: parseFuncs(t, src)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_http_request") || !strings.Contains(c, "mfl_http_result") {
		t.Fatal("http_request must emit the result struct + accessor")
	}
	if !strings.Contains(c, "SSL_new") && !strings.Contains(c, "mfl_tls_dial_e") {
		t.Fatal("http_request must gate in the OpenSSL TLS runtime")
	}
	// single-value misuse errors helpfully, like http_get
	bad := `func main() { h := []string{}  x := http_request("GET", "https://x", h, "")  println(x) }`
	if _, err := CompileToC(&Program{Funcs: parseFuncs(t, bad)}, false); err == nil || !strings.Contains(err.Error(), "returns 3 values") {
		t.Fatalf("single-value http_request should error helpfully, got: %v", err)
	}
}

// The HTTP client speaks both plain http:// (a raw TCP socket) and https:// (TLS)
// from the same path — so the generated client must contain the plain-socket
// transport, and must NOT keep the old "scheme" rejection.
func TestHTTPPlainAndTLS(t *testing.T) {
	c, err := CompileToC(&Program{Funcs: parseFuncs(t, `func main() { s, b, e := http_get("http://x")  println(str(s) + b + e) }`)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_tcp_dial_e") || !strings.Contains(c, "mfl_sock_readall") {
		t.Fatal("the HTTP client must emit the plain-socket transport")
	}
	if !strings.Contains(c, "mfl_tls_dial_e") {
		t.Fatal("the HTTP client must still emit the TLS transport")
	}
	if strings.Contains(c, `R.err = mfl_dup_arena("scheme"`) {
		t.Fatal("the old http:// 'scheme' rejection must be gone")
	}
}

// A string (or a struct's string fields) allocated in a short-lived goroutine
// and sent over a channel must survive that goroutine's arena being reclaimed —
// the channel deep-copies strings on send and adopts them on receive.
func TestChannelStringArena(t *testing.T) {
	// prod allocates each string in its own goroutine arena, sends it, then
	// returns (arena reclaimed) — work and main must still see valid strings.
	prod := `func prod(jobs) { i := 0 for i < 4 { jobs <- "u-" + str(i) i = i + 1 } close(jobs) }`
	work := `func work(jobs, out) { for u := range jobs { out <- u + "!" } close(out) }`
	main := `func main() {
	jobs := make(chan string) out := make(chan string)
	go work(jobs, out)
	go prod(jobs)
	acc := ""
	for v := range out { acc = acc + v + " " }
	println(acc)
}`
	out, _ := buildRun(t, main, prod, work)
	if out != "u-0! u-1! u-2! u-3! \n" {
		t.Fatalf("channel string arena: got %q", out)
	}
}

// Slices and maps sent over a channel from a short-lived goroutine are deep-
// copied (via a JSON round-trip), so their backings survive the sender's arena.
func TestChannelSliceMap(t *testing.T) {
	prodS := `func prodS(jobs) { i := 0 for i < 3 { jobs <- []string{"a" + str(i), "b" + str(i)} i = i + 1 } close(jobs) }`
	prodM := `func prodM(jobs) { i := 0 for i < 2 { m := make(map[string]int) m["k"] = i * 7 jobs <- m i = i + 1 } close(jobs) }`
	main := `func main() {
	sj := make(chan []string)
	go prodS(sj)
	acc := ""
	for s := range sj { for _, e := range s { acc = acc + e } acc = acc + " " }
	println(acc)
	mj := make(chan map[string]int)
	go prodM(mj)
	macc := ""
	for m := range mj { macc = macc + str(m["k"]) + " " }
	println(macc)
}`
	out, _ := buildRun(t, main, prodS, prodM)
	if out != "a0b0 a1b1 a2b2 \n0 7 \n" {
		t.Fatalf("channel slice/map: got %q", out)
	}
}

// A string (or a struct's string fields) built in a short-lived goroutine and
// passed as a `go` call ARGUMENT (not sent over a channel) must survive that
// goroutine's arena being reclaimed -- the exact machweb pattern from #310:
// `go background_work(ag, conv)` then the handler returns. Stress it with
// concurrent spawns + allocation churn (the freed arena's memory must be
// likely to get reused before the corruption would show up on the old,
// unprotected codegen -- validated manually: ~25/30 corrupted on the old
// codegen, 0/30 on the fixed one, across repeated runs). Results are
// collected over a channel and printed once from main, single-threaded --
// concurrent println from 30 goroutines is its own (unrelated) source of
// interleaved stdout, which would make this test flaky for a reason that has
// nothing to do with #310.
func TestGoArgStringSurvivesSpawnerArena(t *testing.T) {
	typ := `type Agent struct { name string  greeting string }`
	work := `func background_work(ag, conv, results) {
	sleep(15)
	results <- "conv=" + conv + " name=" + ag.name + " greet=" + ag.greeting
}`
	spawn := `func spawn_one(n, results) {
	conv := "conv-id-0123456789-" + str(n)
	ag := Agent{name: "assistant-name-padding-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", greeting: "hello-greeting-padding-yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy"}
	go background_work(ag, conv, results)
}`
	churn := `func churn(n) {
	i := 0
	for i < 400 {
		s := "garbage-churn-padding-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-" + str(n) + "-" + str(i)
		if len(s) < 0 { println(s) }
		i = i + 1
	}
}`
	main := `func main() {
	results := make(chan string)
	i := 0
	for i < 30 {
		go spawn_one(i, results)
		go churn(i)
		i = i + 1
	}
	acc := ""
	j := 0
	for j < 30 { acc = acc + <-results + "\n" j = j + 1 }
	println(acc)
}`
	out := runProg(t, typ, work, spawn, churn, main)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 30 {
		t.Fatalf("expected 30 lines, got %d:\n%s", len(lines), out)
	}
	seen := map[string]bool{}
	for _, l := range lines {
		if !strings.Contains(l, "name=assistant-name-padding-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx") ||
			!strings.Contains(l, "greet=hello-greeting-padding-yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy") ||
			!strings.Contains(l, "conv=conv-id-0123456789-") {
			t.Fatalf("corrupted go-call argument (use-after-free, #310): %q", l)
		}
		seen[l] = true
	}
	if len(seen) != 30 {
		t.Fatalf("expected 30 distinct conv ids, got %d (duplicate = aliased/corrupted memory):\n%s", len(seen), out)
	}
}

// A struct field that is a slice, passed as a `go` call argument, must survive
// the spawning goroutine's arena via the JSON round-trip path (the same one
// channels use for slice/map elements) -- #310's other codegen branch.
// Results are collected over a channel and printed once, for the same
// stdout-interleaving reason as TestGoArgStringSurvivesSpawnerArena.
func TestGoArgSliceSurvivesSpawnerArena(t *testing.T) {
	typ := `type Bag struct { tags []string  id int }`
	consume := `func consume(b, results) {
	sleep(12)
	s := ""
	i := 0
	for i < len(b.tags) { s = s + b.tags[i] + "," i = i + 1 }
	results <- "id=" + str(b.id) + " tags=" + s
}`
	spawn := `func spawn_one(n, results) {
	b := Bag{tags: []string{"alpha-" + str(n), "beta-" + str(n), "gamma-" + str(n)}, id: n}
	go consume(b, results)
}`
	churn := `func churn(n) {
	i := 0
	for i < 300 {
		s := "garbage-" + str(n) + "-" + str(i) + "-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
		if len(s) < 0 { println(s) }
		i = i + 1
	}
}`
	main := `func main() {
	results := make(chan string)
	i := 0
	for i < 20 {
		go spawn_one(i, results)
		go churn(i)
		i = i + 1
	}
	acc := ""
	j := 0
	for j < 20 { acc = acc + <-results + "\n" j = j + 1 }
	println(acc)
}`
	out := runProg(t, typ, consume, spawn, churn, main)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 20 {
		t.Fatalf("expected 20 lines (old codegen segfaults on this pattern), got %d:\n%s", len(lines), out)
	}
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		want := fmt.Sprintf("id=%d tags=alpha-%d,beta-%d,gamma-%d,", i, i, i, i)
		found := false
		for _, l := range lines {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing or corrupted %q in:\n%s", want, out)
		}
		seen[want] = true
	}
	if len(seen) != 20 {
		t.Fatalf("expected 20 distinct ids, got %d", len(seen))
	}
}

// close(ch) ends a range-over-channel: workers draining a closed jobs channel
// stop cleanly, and a closed buffered channel is drained before the range ends.
func TestChannelClose(t *testing.T) {
	sq := `func sq(jobs, out) { for j := range jobs { out <- j*j } out <- -1 }`
	main := `func main() {
	jobs := make(chan int) out := make(chan int)
	go sq(jobs, out)
	n := 1 for n <= 4 { jobs <- n n = n + 1 }
	close(jobs)
	sum := 0 done := false
	for done == false { r := <- out if r == -1 { done = true } if r != -1 { sum = sum + r } }
	println(str(sum))
	buf := make(chan int) buf <- 10 buf <- 20 close(buf)
	t := 0 for v := range buf { t = t + v }
	println(str(t))
}`
	out, _ := buildRun(t, main, sq)
	if out != "30\n30\n" { // 1+4+9+16=30 ; 10+20=30
		t.Fatalf("close/range: got %q, want %q", out, "30\n30\n")
	}
}

// flush() forces buffered stdout out; it compiles to fflush and is a no-op on
// captured output (which sees everything anyway), so just assert it runs.
func TestFlush(t *testing.T) {
	prog := &Program{Funcs: parseFuncs(t, `func main() { print("a") flush() print("b") flush() }`)}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_flush()") {
		t.Fatal("flush() must compile to mfl_flush()")
	}
	out, _ := buildRun(t, `func main() { print("a") flush() print("b") flush() }`)
	if out != "ab" {
		t.Fatalf("flush: got %q, want %q", out, "ab")
	}
}

// SQLite: open in-memory, create/insert, and query back a JSON row array
// (INTEGER unquoted, TEXT escaped) — links libsqlite3 only because sqlite_* is used.
func TestSQLite(t *testing.T) {
	main := `func main() {
	db := sqlite_open(":memory:")
	sqlite_exec(db, "CREATE TABLE t(n int, s text)")
	sqlite_exec(db, "INSERT INTO t VALUES(1, 'a')")
	sqlite_exec(db, "INSERT INTO t VALUES(2, 'b')")
	println(sqlite_query(db, "SELECT n, s FROM t ORDER BY n"))
	sqlite_close(db)
}`
	out, _ := buildRun(t, main)
	if out != "[{\"n\":1,\"s\":\"a\"},{\"n\":2,\"s\":\"b\"}]\n" {
		t.Fatalf("sqlite: got %q", out)
	}
}

// Parameterized queries bind a []string to the ? placeholders — injection-safe
// (a value containing SQL is stored literally, not executed).
func TestSQLiteParams(t *testing.T) {
	main := `func main() {
	db := sqlite_open(":memory:")
	sqlite_exec(db, "CREATE TABLE u(name TEXT)")
	sqlite_exec(db, "INSERT INTO u VALUES(?)", []string{"'; DROP TABLE u; --"})
	r := sqlite_query(db, "SELECT name FROM u WHERE name = ?", []string{"'; DROP TABLE u; --"})
	println(r)
	sqlite_close(db)
}`
	out, _ := buildRun(t, main)
	if out != "[{\"name\":\"'; DROP TABLE u; --\"}]\n" {
		t.Fatalf("sqlite params: got %q", out)
	}
}

// url_encode follows RFC 3986 (space -> %20, reserved chars escaped, unreserved
// kept); url_decode reverses it and also treats '+' as space.
func TestURLEncoding(t *testing.T) {
	main := `func main() {
	println(url_encode("a b&c=d/e?f+g~h"))
	println(url_decode("a%20b%26c%3Dd%2Fe%3Ff%2Bg~h"))
	println(url_decode("a+b%20c"))
	println(url_decode(url_encode("héllo, wörld! 100%")))
}`
	out, _ := buildRun(t, main)
	want := "a%20b%26c%3Dd%2Fe%3Ff%2Bg~h\n" +
		"a b&c=d/e?f+g~h\n" +
		"a b c\n" +
		"héllo, wörld! 100%\n"
	if out != want {
		t.Fatalf("url encoding: got %q, want %q", out, want)
	}
}

// bytes is a NUL-safe binary buffer: it must survive an embedded zero byte that
// would truncate a string, round-trip through hex, and support len/byte_at/
// sub/concat. The "00" in the middle is the whole point.
func TestBytes(t *testing.T) {
	main := `func main() {
	b := from_hex("48650061ff")            // H e \0 a 0xff  — has an embedded NUL
	println(str(len(b)))                    // 5, not 2 (a string would stop at the NUL)
	println(to_hex(b))
	println(str(byte_at(b, 4)))             // 255
	println(to_hex(bytes_sub(b, 1, 3)))     // "6500"
	println(to_hex(bytes_concat(from_hex("aa"), from_hex("bbcc"))))
	println(to_hex(bytes("ABC")))           // 414243
	println(b)                              // println of bytes prints hex
}`
	out, _ := buildRun(t, main)
	want := "5\n48650061ff\n255\n6500\naabbcc\n414243\n48650061ff\n"
	if out != want {
		t.Fatalf("bytes: got %q, want %q", out, want)
	}
}

// bytes is a first-class declarable type: usable as a struct field (and mutable),
// a map value, and a function return — not just an inferred local.
func TestBytesDeclarable(t *testing.T) {
	out := runProg(t,
		`type Box struct { id int  data bytes }`,
		`func main() {
	b := Box{id: 7, data: from_hex("deadbeef")}
	b.data = bytes_concat(b.data, from_hex("99"))
	m := make(map[string]bytes)
	m["k"] = from_hex("00ff")
	println(str(b.id) + " " + to_hex(b.data) + " " + to_hex(m["k"]))
}`)
	if out != "7 deadbeef99 00ff\n" {
		t.Fatalf("declarable bytes: got %q", out)
	}
}

// The OpenSSL-backed crypto builtins over bytes: digests match known vectors,
// X25519 agreement holds, Ed25519 sign/verify works (and rejects tampering), and
// AES-GCM round-trips (with auth failure -> empty on a wrong AAD).
func TestCrypto(t *testing.T) {
	main := `func main() {
	println(to_hex(sha256_bytes(bytes("abc"))))
	println(to_hex(hmac_sha256_bytes(bytes("key"), bytes("msg"))))
	a := rand_bytes(32)  b := rand_bytes(32)
	println(to_hex(x25519_shared(a, x25519_pub(b))) == to_hex(x25519_shared(b, x25519_pub(a))))
	seed := rand_bytes(32)  m := bytes("hi")
	sig := ed25519_sign(seed, m)
	println(ed25519_verify(ed25519_pub(seed), m, sig))
	println(ed25519_verify(ed25519_pub(seed), bytes("no"), sig))
	k := rand_bytes(32)  iv := rand_bytes(12)
		ct := aes_gcm_encrypt(k, iv, m, bytes(""))
	println(bytes_str(aes_gcm_decrypt(k, iv, ct, bytes(""))) == "hi")
	println(str(len(aes_gcm_decrypt(k, iv, ct, bytes("x")))))
	// pbkdf2_sha256: deterministic, correct length, different salt → different hash
	println(to_hex(pbkdf2_sha256(bytes("password"), bytes("salt"), 1, 20)) == to_hex(pbkdf2_sha256(bytes("password"), bytes("salt"), 1, 20)))
	println(str(len(to_hex(pbkdf2_sha256(bytes("password"), bytes("salt"), 1, 20)))) == "40")
}`
	out, _ := buildRun(t, main)
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n" +
		"2d93cbc1be167bcb1637a4a23cbff01a7878f0c50ee833954ea5221bb1b8c628\n" +
		"true\ntrue\nfalse\ntrue\n0\ntrue\ntrue\n"
	if out != want {
		t.Fatalf("crypto: got %q, want %q", out, want)
	}
}

// Keccak-256 (Ethereum's hash, distinct from NIST SHA3-256) against the two
// canonical published test vectors.
func TestKeccak256(t *testing.T) {
	main := `func main() {
	println(to_hex(keccak256(bytes(""))))
	println(to_hex(keccak256(bytes("abc"))))
}`
	out, _ := buildRun(t, main)
	want := "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470\n" +
		"4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45\n"
	if out != want {
		t.Fatalf("keccak256: got %q, want %q", out, want)
	}
}

// secp256k1 (Ethereum signing): pubkey derivation against the well-known
// generator-point vector (priv=1 -> pub=G), a cross-check against a signature
// independently produced by Python's coincurve (a real libsecp256k1 binding —
// a different codebase than OpenSSL, so this catches curve/recovery-math bugs
// a self-consistent OpenSSL-only round trip could hide), and a self round-trip
// (sign then recover reproduces the signer's own pubkey).
func TestSecp256k1(t *testing.T) {
	main := `func main() {
	// priv=1 -> pub is the secp256k1 generator point G (a public constant)
	g := secp256k1_pubkey(from_hex("0000000000000000000000000000000000000000000000000000000000000001"))
	println(to_hex(g))

	// cross-check vs a signature generated independently by Python coincurve/libsecp256k1
	priv := from_hex("307fb3b1869f707a2ca5136904ed3c1de1d5ce097b493de4f303d9adcf07b748")
	expectedPub := "042a9d3210951b9765dbab10f8e846b4d1835053c2e58dfcf41cfe437d03e7ca0740289a468bbb05ec363aeafd881a3b2ba9d38d37f402366993d9ddb026ea4887"
	println(to_hex(secp256k1_pubkey(priv)) == expectedPub)
	hash := from_hex("da8f53d107c2678f816c59ae65d92a666e1aa8377fb97ba3854607860e80bc7f")
	rs := from_hex("36878653aa09d6e72d2cd43574b1e4324268765645d9b7393888d72f8c69d93f28e99499d061809b07adcd0bb75491ea7dac64851705053315af742ce46b35b4")
	sig := bytes_concat(rs, from_hex("1b"))
	println(to_hex(secp256k1_recover(hash, sig)) == expectedPub)

	// self round-trip: sign with a fresh random key, recover reproduces the pubkey
	p2 := rand_bytes(32)
	pub2 := secp256k1_pubkey(p2)
	msg := keccak256(bytes("machin eip712 self-test"))
	sig2 := secp256k1_sign_recoverable(p2, msg)
	println(str(len(sig2)))
	v := byte_at(sig2, 64)
	println(v == 27 || v == 28)
	println(to_hex(secp256k1_recover(msg, sig2)) == to_hex(pub2))

	// a bad signature must not recover (empty bytes, not a garbage point)
	println(str(len(secp256k1_recover(msg, from_hex("00")))))
}`
	out, _ := buildRun(t, main)
	want := "0479be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8\n" +
		"true\ntrue\n65\ntrue\ntrue\n0\n"
	if out != want {
		t.Fatalf("secp256k1: got %q, want %q", out, want)
	}
}

// XEdDSA (Curve25519 signatures) is gated like the other external runtimes:
// emitted only when xeddsa_* is used. Compile-level (linking needs libsodium-dev,
// so the behavioral check lives in machin-signal/machin-wapair, not the suite).
func TestXEdDSAGating(t *testing.T) {
	src := `func main() { sig := xeddsa_sign(rand_bytes(32), bytes("m"), rand_bytes(64))  println(xeddsa_verify(rand_bytes(32), bytes("m"), sig)) }`
	c, err := CompileToC(&Program{Funcs: parseFuncs(t, src)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_xeddsa_sign") || !strings.Contains(c, "mfl_xeddsa_verify") || !strings.Contains(c, "mflx_mont_to_ed") {
		t.Fatal("a program using xeddsa_* must emit the XEdDSA runtime")
	}
	// a crypto-free program must not pull it in
	c2, err := CompileToC(&Program{Funcs: parseFuncs(t, `func main() { println("hi") }`)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c2, "mfl_xeddsa_") {
		t.Fatal("a program not using xeddsa_* must not emit the XEdDSA runtime")
	}
}

// Bitwise operators (& | ^ << >> and unary ^) plus hex/binary/octal int literals:
// precedence matches Go (<< >> & like * / %; | ^ like + -), int-only.
func TestBitwise(t *testing.T) {
	main := `func main() {
	println(str(0xff & 0x0f) + " " + str(0xf0 | 0x0f) + " " + str(0xff ^ 0x0f))
	println(str(1 << 8) + " " + str(0xabcd >> 8) + " " + str(^0))
	println(str(0b1010) + " " + str(0o17) + " " + str(0xa5 >> 4 & 0x0f))
	println(str(1 | 2 << 1))
}`
	out, _ := buildRun(t, main)
	want := "15 255 240\n256 171 -1\n10 15 10\n5\n"
	if out != want {
		t.Fatalf("bitwise: got %q, want %q", out, want)
	}
	// int-only: a float operand is a clean type error, not leaked cc output
	bad := `func main() { x := 1.5  println(str(x & 2)) }`
	if _, err := CompileToC(&Program{Funcs: parseFuncs(t, bad)}, false); err == nil {
		t.Fatal("bitwise on a float operand should be a type error")
	}
}

// issue #208: `encode` tightens whitespace, so `x < -1` becomes byte-adjacent
// `x<-1` in canonical form — and the lexer used to greedily merge `<` and `-`
// into a single channel-receive token there, so a valid comparison against a
// negative literal failed to parse AFTER the round trip through canonical
// form. buildRun exercises the real pipeline (loose source -> tighten -> lex
// -> parse -> compile -> run), the same path that broke.
func TestLessThanNegative(t *testing.T) {
	main := `func main() {
	x := 5
	if x < -1 { println("neg") } else { println("ok") }
	h := 0.5
	if h < -0.7 { println("below") } else { println("above") }
	// precedence: && binds looser than <, so this must parse as a && (b < -1)
	a := true
	b := 10
	if a && b < -1 { println("wrong") } else { println("precedence-ok") }
	// the implicit unary minus still composes with what follows it
	c := 5
	if c < -1 + 10 { println("chain-ok") } else { println("chain-wrong") }
}`
	out, _ := buildRun(t, main)
	want := "ok\nabove\nprecedence-ok\nchain-ok\n"
	if out != want {
		t.Fatalf("x < -1 after tightening: got %q, want %q", out, want)
	}
}

// Same fix, non-regression: `ch <- v` (channel send) is lexically the exact
// same ambiguous shape (IDENT immediately followed by `<-`) as `x < -1` in
// canonical form, and must still be recognized as a send, not split into a
// comparison.
func TestLessThanNegativeChannelSendUnaffected(t *testing.T) {
	out := runNative(t,
		`func recv(ch) { v := <-ch  println("got " + str(v)) }`,
		`func main() { ch := make(chan int)  go recv(ch)  ch <- 42  sleep(50) }`)
	if out != "got 42\n" {
		t.Fatalf("channel send regressed: got %q", out)
	}
}

// Opaque FFI handles: `cstruct Name {}` (empty body) wraps a by-value C struct
// machin can hold and pass back without naming its fields — for APIs whose
// structs contain pointers (raylib Sound/Music, ...). Codegen-level (no link).
func TestOpaqueHandle(t *testing.T) {
	src := []string{
		`extern "audio" { header "audio.h" cstruct Sound {} fn LoadSound(string) Sound fn PlaySound(Sound) }`,
		`func main() { s := LoadSound("a.wav")  PlaySound(s) }`,
	}
	prog, err := ParseProgram(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	// the MFL wrapper holds the real C type by value in one hidden field...
	if !strings.Contains(c, "typedef struct { Sound _c; } mfl_Sound;") {
		t.Fatal("opaque handle must wrap the real C type in mfl_Sound._c")
	}
	// ...and marshaling copies the whole struct in/out, not field-by-field
	if !strings.Contains(c, "mfl_to_Sound(mfl_Sound m) { return m._c; }") {
		t.Fatal("opaque handle marshaling must copy the whole C struct")
	}
	if !strings.Contains(c, "mfl_from_Sound") {
		t.Fatal("opaque handle must marshal the FFI return back to MFL")
	}
}

// Native math builtins (libm), linked -lm only when used; an explicit extern of
// the same name shadows the builtin.
func TestMathBuiltins(t *testing.T) {
	main := `func main() {
	println(str(sqrt(9.0)) + " " + str(pow(2.0, 8.0)) + " " + str(floor(3.9)) + " " + str(ceil(3.1)))
	println(str(abs(0.0 - 7.0)) + " " + str(hypot(3.0, 4.0)) + " " + str(round(2.5)))
	println(str(cos(0.0)) + " " + str(atan2(0.0, 1.0)))
}`
	out, _ := buildRun(t, main)
	want := "3 256 3 4\n7 5 3\n1 0\n"
	if out != want {
		t.Fatalf("math: got %q, want %q", out, want)
	}
	// gated: a math-free program emits no math runtime (so it never links -lm)
	plain, err := CompileToC(&Program{Funcs: parseFuncs(t, `func main() { println("hi") }`)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "mfl_math_") {
		t.Fatal("math runtime must not be emitted for a math-free program")
	}
	// an explicit extern declaration shadows the builtin of the same name
	prog, err := ParseProgram([]string{
		`extern "m" { header "math.h" link "m" fn sqrt(float) float }`,
		`func main() { println(str(sqrt(1.0, 2.0))) }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "expected 1 args") {
		t.Fatalf("extern sqrt should shadow the builtin (arity-checked), got %v", err)
	}
}

// Raw heap memory: alloc + typed poke/peek round-trip, then free. (Pointers are
// ints; for building C buffers/structs to hand to a foreign API.)
func TestRawMemory(t *testing.T) {
	main := `func main() {
	p := alloc(32)
	poke_f32(p, 0, 1.5)
	poke_i32(p, 8, 7)
	poke_u8(p, 12, 200)
	println(str(peek_f32(p, 0)) + " " + str(peek_i32(p, 8)) + " " + str(peek_i32(p, 12)))
	free(p)
}`
	out, _ := buildRun(t, main)
	if out != "1.5 7 200\n" {
		t.Fatalf("raw memory: got %q, want %q", out, "1.5 7 200\n")
	}
}

// poke_u16 and poke_ptr had no test coverage. There's no matching peek_u16 /
// peek_ptr builtin, so verify each write via peek_i32 against a zeroed
// region: on a little-endian host the untouched high bytes stay 0, so the
// wrong width or a swapped store would show up as a mismatched readback.
func TestRawMemoryU16AndPtr(t *testing.T) {
	main := `func main() {
	p := alloc(16)
	poke_i32(p, 0, 0)
	poke_i32(p, 8, 0)
	poke_u16(p, 0, 4660)
	poke_ptr(p, 8, 305419896)
	println(str(peek_i32(p, 0)) + " " + str(peek_i32(p, 8)))
	free(p)
}`
	out, _ := buildRun(t, main)
	if out != "4660 305419896\n" {
		t.Fatalf("raw memory u16/ptr: got %q, want %q", out, "4660 305419896\n")
	}
}

// The `*Name` FFI param convention: deref an MFL int (pointer) and pass the
// pointed-to C struct by value (e.g. LoadModelFromMesh(*Mesh)). Codegen-level.
func TestFFIDerefParam(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "g" { header "g.h" cstruct Model {} fn LoadFrom(*Thing) Model }`,
		`func main() { m := LoadFrom(0)  println(str(0)) }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "(*(Thing*)(intptr_t)") {
		t.Fatal("*Name param must deref the pointer and pass the C struct by value")
	}
}

// A cstruct field may be a pointer (`ptr`) — held as an int in MFL, cast through
// void* at the boundary (so the C compiler lays out a struct like raylib's Mesh).
func TestPointerCStructField(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "g" { header "g.h" cstruct Buf { n i32 data ptr } fn Use(Buf) }`,
		`func main() { b := Buf{4, alloc(16)}  Use(b)  println(str(b.n)) }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, ".data = (void*)(intptr_t)m.f_data") {
		t.Fatal("a ptr cstruct field must marshal through void*")
	}
}

// `Name*` (inout): pass an MFL cstruct by pointer and write the modified struct
// back to the variable after the call (e.g. UploadMesh(Mesh*)).
func TestInoutStructParam(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "g" { header "g.h" cstruct S { n i32 } fn Up(S*) }`,
		`func main() { s := S{1}  Up(s)  println(str(s.n)) }`,
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_to_S(v_s)") || !strings.Contains(c, "= mfl_from_S(_io") || !strings.Contains(c, "Up(&_io") {
		t.Fatal("Name* must pass &temp and write the struct back to the variable")
	}
}

// Nested cstructs: a cstruct field whose type is another cstruct (by-value
// struct of by-value structs — e.g. raylib Camera3D holds Vector3s). The MFL
// wrapper nests the inner mfl_ type and marshaling recurses. Codegen-level.
func TestNestedCStruct(t *testing.T) {
	src := []string{
		`extern "g" { header "g.h" cstruct V3 { x f32 y f32 z f32 } cstruct Cam { pos V3 fovy f32 } fn Begin(Cam) }`,
		`func main() { c := Cam{V3{1.0, 2.0, 3.0}, 45.0}  Begin(c) }`,
	}
	prog, err := ParseProgram(src)
	if err != nil {
		t.Fatal(err)
	}
	c, err := CompileToC(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_V3 f_pos;") {
		t.Fatal("nested cstruct field must be the inner mfl_ type")
	}
	if !strings.Contains(c, "mfl_from_V3(c.pos)") || !strings.Contains(c, "mfl_to_V3(m.f_pos)") {
		t.Fatal("nested cstruct field must marshal recursively")
	}
}

// Perlin noise builtins: deterministic, in ~[-1,1], continuous; gated (-lm only
// when used).
func TestNoise(t *testing.T) {
	main := `func main() {
	a := noise2(3.5, 1.2)
	b := noise2(3.5, 1.2)
	ok := "no"  if a == b { ok = "yes" }
	inr := "no"  if a > 0.0 - 1.0 { if a < 1.0 { inr = "yes" } }
	near := "no"  d := noise2(3.5, 1.2) - noise2(3.51, 1.2)  if d < 0.2 { if d > 0.0 - 0.2 { near = "yes" } }
	println(ok + " " + inr + " " + near + " " + str(noise3(0.0, 0.0, 0.0)))
}`
	out, _ := buildRun(t, main)
	// deterministic, in range, continuous; noise3 at the origin is ~0
	if !strings.HasPrefix(out, "yes yes yes ") {
		t.Fatalf("noise: got %q", out)
	}
	// gated: a noise-free program emits no noise runtime
	plain, err := CompileToC(&Program{Funcs: parseFuncs(t, `func main() { println("hi") }`)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "mfl_noise") {
		t.Fatal("noise runtime must not be emitted for a noise-free program")
	}
}

// float() lifts a concrete int into float arithmetic (the counterpart to int()).
// MFL has no implicit int->float, so a value typed int (a function return,
// byte_at, len, ...) can't mix with a float without this.
func TestFloatCast(t *testing.T) {
	main := `func main() {
	xs := []int{7, 0, 0}
	avg := float(xs[0]) / 2.0
	println(str(avg) + " " + str(float(3.5)) + " " + str(int(float(9))))
}`
	out, _ := buildRun(t, main)
	want := "3.5 3.5 9\n"
	if out != want {
		t.Fatalf("float: got %q, want %q", out, want)
	}
	// float() of a non-number is a type error
	bad := `func main() { println(str(float("x"))) }`
	if _, err := CompileToC(&Program{Funcs: parseFuncs(t, bad)}, false); err == nil {
		t.Fatal("float of a string should be a type error")
	}
}

// str() over each accepted kind: number, bool, and string. Bool renders as
// "true"/"false" (the papercut fix); a non-stringable kind is a clean error.
func TestStrKinds(t *testing.T) {
	main := `func main() {
	println(str(42) + " " + str(3.5))
	println(str(true) + " " + str(false) + " " + str(10 > 3))
	println(str("hi"))
}`
	out, _ := buildRun(t, main)
	want := "42 3.5\ntrue false true\nhi\n"
	if out != want {
		t.Fatalf("str: got %q, want %q", out, want)
	}
	// a slice (or any non-number/bool/string) is still a type error
	bad := `func main() { xs := []int{1, 2}  println(str(xs)) }`
	if _, err := CompileToC(&Program{Funcs: parseFuncs(t, bad)}, false); err == nil {
		t.Fatal("str of a slice should be a type error")
	}
}

// Terminal input builtins: raw_mode (int -> int) and read_key (-> string).
// Interactive behavior can't be unit-tested, so this checks the type contract
// and that the C transport helpers are emitted.
func TestTerminalInput(t *testing.T) {
	src := `func main() { r := raw_mode(1)  k := read_key()  if k == "q" { println("bye " + str(r)) }  raw_mode(0) }`
	c, err := CompileToC(&Program{Funcs: parseFuncs(t, src)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_raw_mode") || !strings.Contains(c, "mfl_read_key") {
		t.Fatal("terminal input must emit mfl_raw_mode / mfl_read_key")
	}
	// raw_mode needs its on/off argument
	bad := `func main() { raw_mode() }`
	if _, err := CompileToC(&Program{Funcs: parseFuncs(t, bad)}, false); err == nil {
		t.Fatal("raw_mode() with no argument should be an arity error")
	}
}

// SHA-256 and HMAC-SHA256 against published test vectors (must be byte-exact).
func TestHashes(t *testing.T) {
	main := `func main() {
	println(sha256("abc"))
	println(hmac_sha256("key", "The quick brown fox jumps over the lazy dog"))
}`
	out, _ := buildRun(t, main)
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n" +
		"f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8\n"
	if out != want {
		t.Fatalf("hash: got %q, want %q", out, want)
	}
}

// base64 encode/decode, round-trip, and lenient url-safe (JWT) decoding.
func TestBase64(t *testing.T) {
	main := `func main() {
	e := base64_encode("hi machin")
	rt := "no"  if base64_decode(e) == "hi machin" { rt = "yes" }
	jwt := base64_decode("eyJhbGciOiJIUzI1NiJ9")
	println(e + "|" + rt + "|" + jwt)
}`
	out, _ := buildRun(t, main)
	if out != "aGkgbWFjaGlu|yes|{\"alg\":\"HS256\"}\n" {
		t.Fatalf("base64: got %q", out)
	}
}

// POSIX regex builtins: match, find, capture groups, and replace-all.
func TestRegex(t *testing.T) {
	main := `func main() {
	ok := "false"  if regex_match("a@b.co", "^[a-z]+@[a-z]+\.[a-z]+$") { ok = "true" }
	g := regex_groups("2026-06-22", "([0-9]+)-([0-9]+)-([0-9]+)")
	gs := ""  if len(g) == 4 { gs = g[1] + "/" + g[2] + "/" + g[3] }
	println(ok + "|" + regex_find("id 4821 x", "[0-9]+") + "|" + gs + "|" + regex_replace("5-9", "[0-9]+", "#"))
}`
	out, _ := buildRun(t, main)
	if out != "true|4821|2026/06/22|#-#\n" {
		t.Fatalf("regex: got %q", out)
	}
}

// A malformed pattern must fail safe per docs/LANGUAGE.md rather than crash:
// match->false, find->"", groups->empty slice, replace->input unchanged.
func TestRegexBadPattern(t *testing.T) {
	main := `func main() {
	ok := "false"  if regex_match("abc", "[a-z") { ok = "true" }
	g := regex_groups("abc", "[a-z")
	println(ok + "|" + regex_find("abc", "[a-z") + "|" + str(len(g)) + "|" + regex_replace("abc", "[a-z", "x"))
}`
	out, _ := buildRun(t, main)
	if out != "false||0|abc\n" {
		t.Fatalf("regex bad pattern: got %q", out)
	}
}

// Operands and arguments evaluate left-to-right (Go semantics), even when they
// have side effects — `g() + g()` on a counter yields 1 then 2, not 2 then 1.
func TestEvalOrderLeftToRight(t *testing.T) {
	mk := `func mk() (f) { n := 0  f = func() { n = n + 1  return n } }`
	two := `func two(a, b) (s) { s = str(a) + "," + str(b) }`
	main := `func main() {
	g := mk()  println(str(g()) + " " + str(g()))
	h := mk()  println(two(h(), h()))
	k := mk()  xs := []int{k(), k(), k()}  println(str(xs[0]) + str(xs[1]) + str(xs[2]))
}`
	out, _ := buildRun(t, main, mk, two)
	if out != "1 2\n1,2\n123\n" {
		t.Fatalf("eval order: got %q, want %q", out, "1 2\n1,2\n123\n")
	}
}

// comma-ok receive (v, ok := <-ch) reports false once a channel is closed and
// drained — both standalone and inside a select case (which fires on close).
func TestCommaOkReceive(t *testing.T) {
	prod := `func prod(ch) { i := 0 for i < 3 { ch <- i * 2 i = i + 1 } close(ch) }`
	// standalone comma-ok loop
	standalone := `func main() { ch := make(chan int) go prod(ch) sleep(30) sum := 0 for { v, ok := <- ch if ok == false { break } sum = sum + v } println(str(sum)) }`
	out, _ := buildRun(t, standalone, prod)
	if out != "6\n" {
		t.Fatalf("standalone comma-ok: got %q, want %q", out, "6\n")
	}
	// comma-ok inside select: fires on close with ok == false
	sel := `func main() { ch := make(chan int) go prod(ch) sleep(30) sum := 0 done := false for done == false { select { case v, ok := <- ch: if ok == false { done = true } if ok { sum = sum + v } } } println(str(sum)) }`
	out2, _ := buildRun(t, sel, prod)
	if out2 != "6\n" {
		t.Fatalf("select comma-ok: got %q, want %q", out2, "6\n")
	}
}

// select waits on multiple channels: it takes a ready receive, falls to default
// when nothing is ready, and supports the timer/result timeout pattern.
func TestSelect(t *testing.T) {
	prod := `func prod(ch, v) { ch <- v }`
	main := `func main() {
	a := make(chan int) b := make(chan int)
	go prod(a, 100)
	sleep(20)
	r := 0
	select { case x := <-a: r = x  case y := <-b: r = y }
	println("got " + str(r))
	e := make(chan int)
	select { case z := <-e: println(str(z))  default: println("default") }
}`
	out, _ := buildRun(t, main, prod)
	if out != "got 100\ndefault\n" {
		t.Fatalf("select: got %q, want %q", out, "got 100\ndefault\n")
	}
}

// json_get(json, path) navigates a jq-style path and returns (value, err) — the
// second multi-return builtin, runnable end-to-end (no network).
func TestJSONGet(t *testing.T) {
	main := `func main() { j := "{\"a\":{\"b\":[10,20,30]},\"n\":\"hi\"}" v, e := json_get(j, ".a.b[1]") println(v + "|" + e) v2, e2 := json_get(j, ".n") println(v2 + "|" + e2) v3, e3 := json_get(j, ".missing") println(v3 + "|" + e3) }`
	out, _ := buildRun(t, main)
	if !strings.Contains(out, "20|\n") || !strings.Contains(out, "\"hi\"|\n") || !strings.Contains(out, "|notfound\n") {
		t.Fatalf("json_get output unexpected: %q", out)
	}
}

// Binary WebSocket frames: wss_send_bin takes a bytes payload, wss_recv_bin
// returns bytes. Compiling must wire the binary framing + bytes type and gate in
// the WS/TLS runtime.
func TestWSSBinary(t *testing.T) {
	src := `func main() { c := wss_open("wss://x")  wss_send_bin(c, from_hex("00ff"))  b := wss_recv_bin(c)  println(to_hex(b)) }`
	c, err := CompileToC(&Program{Funcs: parseFuncs(t, src)}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_wss_send_bin") || !strings.Contains(c, "mfl_wss_recv_bin") {
		t.Fatal("binary wss must emit mfl_wss_send_bin / mfl_wss_recv_bin")
	}
	if !strings.Contains(c, "mfl_bytes") || !strings.Contains(c, "mfl_tls_dial") {
		t.Fatal("binary wss must use the bytes type and the TLS core")
	}
}

// The WebSocket runtime is emitted only when a program calls wss_*; it shares
// the TLS core (mfl_tls_dial) with HTTPS but pulls in the WS framing separately.
func TestWSSRuntimeGating(t *testing.T) {
	ws := &Program{Funcs: parseFuncs(t, `func main() { c := wss_open("wss://x") println(wss_recv(c)) }`)}
	c, err := CompileToC(ws, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c, "mfl_wss_open") || !strings.Contains(c, "mfl_tls_dial") || !strings.Contains(c, "openssl/ssl.h") {
		t.Fatal("a program using wss_* must emit the WebSocket runtime atop the TLS core")
	}
	// HTTPS-only must NOT pull in the WSS framing.
	https := &Program{Funcs: parseFuncs(t, `func main() { println(https_get("https://x")) }`)}
	c2, err := CompileToC(https, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c2, "mfl_wss_") {
		t.Fatal("an HTTPS-only program must not emit the WebSocket runtime")
	}
	if !strings.Contains(c2, "mfl_tls_dial") {
		t.Fatal("HTTPS must build on the shared TLS core (mfl_tls_dial)")
	}
}

// break exits the nearest loop; continue skips to the next iteration — in
// bare for{}, condition for-loops, and range loops.
func TestBreakContinue(t *testing.T) {
	sumUntil := `func sum_until(limit) (s) { s = 0 i := 0 for { if i >= 100 { break } i = i + 1 if i % 2 == 0 { continue } if i > limit { break } s = s + i } }`
	firstEven := `func first_even(xs) (v) { v = -1 for _, x := range xs { if x % 2 == 1 { continue } v = x break } }`
	main := `func main() { println(sum_until(10)) println(first_even([]int{3, 7, 4, 8})) }`
	out, _ := buildRun(t, main, sumUntil, firstEven)
	if out != "25\n4\n" {
		t.Fatalf("break/continue: got %q, want %q", out, "25\n4\n")
	}
}

// Braces inside string literals (e.g. a function that builds JSON) and after
// // comments must not be counted as block delimiters when splitting.
func TestSplitFunctionsBracesInStrings(t *testing.T) {
	src := "func j(v) (s) { s = \"{\\\"x\\\":\" + v + \"}\" }\n\n" +
		"func k() (s) { s = index(b, \"}\") // trailing } in a comment\n}\n"
	fns, err := splitFunctions(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 2 {
		t.Fatalf("expected 2 funcs, got %d: %v", len(fns), fns)
	}
}

// #82: splitFunctions had no direct test for multi-level nesting or its own
// "unbalanced braces" error path (only splitFunctionsLoc — check.go's
// separate, duplicated copy of the same brace-counting loop — had one, via
// TestCheckUnbalancedBraces). A regression in either copy's depth tracking
// would ship undetected.
func TestSplitFunctionsNestedBraces(t *testing.T) {
	src := `func f(xs) (n) {
    n = 0
    for i, v := range xs {
        if v > 0 {
            while n < v {
                n = n + 1
            }
        }
    }
}

func main() { println(f([]int{1, 2})) }
`
	fns, err := splitFunctions(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 2 {
		t.Fatalf("expected 2 funcs (one with 4 levels of nested braces), got %d: %v", len(fns), fns)
	}
}

func TestSplitFunctionsUnbalancedBraces(t *testing.T) {
	_, err := splitFunctions("func broken() {\n    if x {\n")
	if err == nil {
		t.Fatal("expected an unbalanced-braces error, got nil")
	}
	if !strings.Contains(err.Error(), "unbalanced braces") {
		t.Fatalf("expected an 'unbalanced braces' error, got: %v", err)
	}
}

// #82: stripLineComment had zero direct test coverage.
func TestStripLineComment(t *testing.T) {
	cases := []struct{ in, want string }{
		{`x := 1 // trailing comment`, `x := 1 `},
		{`s := "http://example.com"`, `s := "http://example.com"`}, // // inside a string is not a comment
		{`s := "a // b" // real comment`, `s := "a // b" `},
		{`s := "a\"//b" // real comment`, `s := "a\"//b" `}, // escaped quote keeps the string open past the //
		{`no comment here`, `no comment here`},
		{`// whole line is a comment`, ``},
	}
	for _, c := range cases {
		if got := stripLineComment(c.in); got != c.want {
			t.Errorf("stripLineComment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// #82: cmdEncode's core (composeSources, extracted for `machin test` too) had
// no test proving multi-file concatenation preserves declaration order, that
// the result round-trips through loadMFL, or that a type error surfaces
// rather than silently emitting bad output.
func TestComposeSourcesMultiFileOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.src")
	second := filepath.Join(dir, "second.src")
	if err := os.WriteFile(first, []byte(`func a() (s) { s = "A" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(`func main() { println(a()) }`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, err := composeSources([]string{first, second})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	ia, im := strings.Index(out, "func a("), strings.Index(out, "func main(")
	if ia < 0 || im < 0 || ia > im {
		t.Fatalf("expected first.src's decl before second.src's in the composed output, got:\n%s", out)
	}
}

func TestComposeSourcesRoundTripsThroughLoadMFL(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app.src")
	if err := os.WriteFile(src, []byte(`func main() { println("hi from round-trip") }`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, err := composeSources([]string{src})
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	mflPath := filepath.Join(dir, "app.mfl")
	if err := os.WriteFile(mflPath, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	prog, err := loadMFL(mflPath)
	if err != nil {
		t.Fatalf("loadMFL on composeSources' own output: %v", err)
	}
	out2, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out2 != "hi from round-trip\n" {
		t.Fatalf("got %q", out2)
	}
}

func TestComposeSourcesSurfacesTypeError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.src")
	if err := os.WriteFile(src, []byte(`func main() { undefined_function_xyz() }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := composeSources([]string{src}); err == nil {
		t.Fatal("expected a typecheck error for a call to an undefined function, got nil")
	}
}
