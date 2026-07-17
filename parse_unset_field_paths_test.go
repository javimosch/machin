package main

import "testing"

// Companion regression coverage for #449. The core fix (parse() seeds absent
// struct string fields with "" instead of a NULL char*, recursively) lives in
// codegen's jsonParser via stringZeroInits, and parse_null_string_field_test.go
// pins the original repro plus one level of nesting. These cases lock in the
// OTHER codegen paths the same NULL char* could have slipped through: struct
// elements produced by the slice parser, recursion deeper than one level, and
// an absent field consumed as a map key. Each would have dereferenced NULL
// (strlen / hash) the moment the unset field met a string builtin.

// Slice parser path: each element struct is produced by the element parser, so
// its absent string fields must be "" too — not just top-level parse-into-struct.
func TestParseSliceElementUnsetStringFieldIsEmpty(t *testing.T) {
	got := runProg(t,
		`type E struct { a string  b string }`,
		`func main() {
			xs := parse("[{\"a\":\"one\"},{\"b\":\"two\"}]", []E{})
			for _, e := range xs { println("a='" + e.a + "' b='" + e.b + "'") }
			println("ok")
		}`)
	want := "a='one' b=''\na='' b='two'\nok\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// Recursion must hold beyond a single nesting level: a 3-deep chain where every
// intermediate struct is absent from the payload must leave the innermost string
// as "" rather than reaching a NULL through two zeroed parents.
func TestParseDeeplyNestedUnsetStringFieldIsEmpty(t *testing.T) {
	got := runProg(t,
		`type A struct { x string }`,
		`type B struct { a A  y string }`,
		`type C struct { b B  z string }`,
		`func main() {
			c := parse("{\"z\":\"zz\"}", C{})
			println("x.len=" + str(len(c.b.a.x)))
			println("y.len=" + str(len(c.b.y)))
			println("z=" + c.z)
		}`)
	want := "x.len=0\ny.len=0\nz=zz\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// An absent field used as a map key must hash an empty string, not deref NULL.
func TestParseUnsetStringFieldUsableAsMapKey(t *testing.T) {
	got := runProg(t,
		`type K struct { key string }`,
		`func main() {
			k := parse("{}", K{})
			m := make(map[string]int)
			m[k.key] = 7
			println("v=" + str(m[k.key]))
			println("keylen=" + str(len(k.key)))
		}`)
	want := "v=7\nkeylen=0\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
