package main

import (
	"strings"
	"testing"
)

// #520: json() built its result with chained mfl_cat (each recopied the whole
// accumulated prefix), so serializing a large []struct was O(n^2) time and left
// O(n^2) arena garbage — a 2 MB output cost ~5 s and 8.2 GB RSS. The serializers
// now use a growable string builder (mfl_sb), so it's O(total output). These
// tests lock in exact-byte correctness through the rewrite and exercise the
// large-slice path that used to blow up.

func TestJSONExactBytesNested(t *testing.T) {
	inner := `type Inner struct { a int  b bool }`
	outer := `type Outer struct { name string  tags []string  scores map[string]int  kid Inner }`
	main := `func main() {
	tg := []string{}
	tg = append(tg, "p")
	tg = append(tg, "q")
	sc := make(map[string]int)
	sc["z"] = 3
	o := Outer{ name: "hi\"x", tags: tg, scores: sc, kid: Inner{a: 0 - 7, b: true} }
	println(json(o))
}`
	out := strings.TrimSpace(buildRunResetProg(t, inner, outer, main))
	want := `{"name":"hi\"x","tags":["p","q"],"scores":{"z":3},"kid":{"a":-7,"b":true}}`
	if out != want {
		t.Fatalf("json bytes changed:\n got %q\nwant %q", out, want)
	}
}

// A large []struct must serialize to the correct total length (and, implicitly,
// complete quickly / in bounded memory — before the fix this was a multi-second,
// multi-gigabyte O(n^2) blowup).
func TestJSONLargeSliceScale(t *testing.T) {
	typ := `type C struct { id int  text string }`
	main := `func main() {
	xs := []C{}
	i := 0
	while i < 20000 { xs = append(xs, C{id: i, text: "payload"})  i = i + 1 }
	s := json(xs)
	println(str(len(s)))
}`
	out := strings.TrimSpace(buildRunResetProg(t, typ, main))
	// Each element: {"id":<i>,"text":"payload"} ; digits of i vary. Compute expected.
	// fixed per element besides the id digits: `{"id":,"text":"payload"}` = 24 chars
	// plus commas between elements (n-1) plus the surrounding [].
	n := 20000
	total := 2 + (n - 1) // [] + commas
	for i := 0; i < n; i++ {
		total += 24 + len(itoa(i))
	}
	if out != itoa(total) {
		t.Fatalf("large-slice json length wrong: got %s want %d", out, total)
	}
}
