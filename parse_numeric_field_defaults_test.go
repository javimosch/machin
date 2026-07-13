package main

import (
	"strings"
	"testing"
)

// parse() populates a struct's numeric and bool fields from JSON, and leaves
// every field the JSON omits at its zero value: 0 for int, 0.0 for float, false
// for bool. Unlike the string-field case (a NULL char* that must be forced to ""
// so string ops don't deref it — see the struct-literal / parse string-field
// contracts), an omitted numeric or bool slot is C-zeroed to a perfectly usable
// 0/false, so the contract here is that the zero value is exactly that and never
// leaks a garbage or sentinel value.
//
// The oracle also pins str()'s numeric formatting through the parsed values: a
// negative int round-trips as "-7", a fractional float as "3.5", a whole-valued
// float ("2.0" in JSON) collapses to "2", a zero float prints "0", and a bool is
// "true"/"false". These are captured from the running binary, not a prior test
// run — every number below is hand-checkable against the JSON input.
func TestParseNumericFieldDefaults(t *testing.T) {
	prog := progFromSrc(t, `
type Rec struct { n int  neg int  f float  ok bool  no bool  s string }
func main() {
    r := parse("{\"n\":42,\"neg\":-7,\"f\":3.5,\"ok\":true,\"no\":false,\"s\":\"hi\"}", Rec{})
    println("present n=" + str(r.n) + " neg=" + str(r.neg) + " f=" + str(r.f) + " ok=" + str(r.ok) + " no=" + str(r.no) + " s=" + r.s)
    a := parse("{\"s\":\"x\"}", Rec{})
    println("absent n=" + str(a.n) + " neg=" + str(a.neg) + " f=" + str(a.f) + " ok=" + str(a.ok) + " no=" + str(a.no))
    p := parse("{\"n\":5,\"f\":2.0}", Rec{})
    println("partial n=" + str(p.n) + " f=" + str(p.f) + " ok=" + str(p.ok))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := strings.Join(strings.Fields(out), " ")
	want := strings.Join(strings.Fields(`
present n=42 neg=-7 f=3.5 ok=true no=false s=hi
absent n=0 neg=0 f=0 ok=false no=false
partial n=5 f=2 ok=false`), " ")
	if got != want {
		t.Fatalf("parse() numeric/bool field defaults:\ngot:  %q\nwant: %q", got, want)
	}
}
