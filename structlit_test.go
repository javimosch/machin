package main

import (
	"strings"
	"testing"
)

// An omitted string field in a struct literal must be "" — a string's zero value —
// not a NULL char* that crashes every string op (compare/concat/len/substr/print).
// C zeroes an omitted compound-literal field to NULL; codegen now fills omitted
// string fields with "" explicitly. (Surfaced by the SSO/machweb dogfood: a handler
// returning a Response with an unset `location` field segfaulted on `!= ""`.)
func TestStructOmittedStringFieldIsEmpty(t *testing.T) {
	prog := progFromSrc(t, `
type Inner struct { tag string }
type T struct { a string  xs []string  loc string  inner Inner }
func main() {
    r := T{a: "hi"}                       // loc, xs, inner all omitted
    println("eq=" + str(r.loc == ""))     // comparison must not deref NULL
    println("len=" + str(len(r.loc)))     // len of the empty string
    println("cat=[" + r.loc + r.a + "]")  // concat
    println("sub=[" + substr(r.loc, 0, 0) + "]")
    println("inner=[" + r.inner.tag + "]") // nested omitted string also ""
    z := T{}                              // wholly empty literal
    println("z=[" + z.a + "|" + z.loc + "|" + z.inner.tag + "] n=" + str(len(z.xs)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.Join(strings.Fields(out), " ")
	want := "eq=true len=0 cat=[hi] sub=[] inner=[] z=[||] n=0"
	if got != want {
		t.Fatalf("omitted string fields not zeroed to \"\":\ngot:  %q\nwant: %q", got, want)
	}
}

// The runtime string operators are also NULL-tolerant directly (defense for any
// other auto-zeroed slot — e.g. a map's default value for an absent key).
func TestStringOpsNullSafe(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    m := make(map[string]string)
    v := m["absent"]                      // absent key -> zero value
    println("eq=" + str(v == ""))
    println("cat=[" + v + "x]")
    println("len=" + str(len(v)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Join(strings.Fields(out), " "); got != "eq=true cat=[x] len=0" {
		t.Fatalf("string ops on an absent-map-key value = %q", got)
	}
}
