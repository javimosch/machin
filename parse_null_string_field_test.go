package main

import "testing"

// Regression for #449: parse() into a struct must default any JSON-absent string
// field to "" (not a NULL char*). A NULL field crashes the moment it reaches
// len()/concat — e.g. strlen(NULL) — which surfaced as a segfault when an unset
// field flowed through a helper and back into a range loop. The struct-literal
// path already seeds "" via stringZeroInits; the JSON parser must do the same.

func TestParseUnsetStringFieldIsEmpty(t *testing.T) {
	got := runProg(t,
		`type T2 struct { a string  b string  c string }`,
		`func getv(t, name) (v) { if name == "a" { v = t.a } if name == "b" { v = t.b } if name == "c" { v = t.c } }`,
		`func main() {
			t := parse("{\"b\":\"hello\"}", T2{})
			for _, name := range []string{"a", "b", "c"} {
				v := getv(t, name)
				println("tok " + name + "='" + v + "'")
				if len(v) == 0 { continue }
				println("use " + name)
			}
			println("done")
		}`)
	want := "tok a=''\ntok b='hello'\nuse b\ntok c=''\ndone\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// The same "" default must hold recursively for nested struct fields left unset
// by the JSON payload — accessing inner.x on an absent inner must not deref NULL.
func TestParseUnsetNestedStructStringFieldIsEmpty(t *testing.T) {
	got := runProg(t,
		`type Inner struct { x string  y string }`,
		`type Outer struct { name string  in Inner  n int }`,
		`func main() {
			o := parse("{\"n\":5}", Outer{})
			println("name.len=" + str(len(o.name)))
			println("in.x.len=" + str(len(o.in.x)))
			println("n=" + str(o.n))
		}`)
	want := "name.len=0\nin.x.len=0\nn=5\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
