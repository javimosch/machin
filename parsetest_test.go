package main

import "testing"

// collectStructNames scans raw source lines for `type NAME` and `cstruct NAME`
// declarations, feeding the set used to recognize `T{...}` composite literals
// while parsing. It must find both forms, ignore unrelated lines, and not be
// confused by a bare keyword with no following identifier.
func TestCollectStructNames(t *testing.T) {
	lines := []string{
		"package main",
		"type Point struct { x int y int }",
		"extern \"c\" {",
		"  cstruct CPoint { x int y int }",
		"}",
		"func main() { p := Point{x: 1, y: 2} }",
		"type", // no following identifier — must not panic or add a bogus entry
	}
	got := collectStructNames(lines)
	want := map[string]bool{"Point": true, "CPoint": true}
	if len(got) != len(want) {
		t.Fatalf("collectStructNames(%v) = %v, want %v", lines, got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("collectStructNames missing %q, got %v", name, got)
		}
	}
}

func TestCollectStructNamesEmpty(t *testing.T) {
	got := collectStructNames(nil)
	if len(got) != 0 {
		t.Fatalf("collectStructNames(nil) = %v, want empty", got)
	}
}

// sexprFields/sexprType are the leaf S-expression renderers the MFL
// self-hosted parser (selfhost/parse.src) must match byte-for-byte.
func TestSexprFields(t *testing.T) {
	got := sexprFields([]string{"x", "y"}, []string{"int", "string"})
	want := "(fields (f x int) (f y string))"
	if got != want {
		t.Fatalf("sexprFields = %q, want %q", got, want)
	}
	if got := sexprFields(nil, nil); got != "(fields)" {
		t.Fatalf("sexprFields(nil, nil) = %q, want %q", got, "(fields)")
	}
}

func TestSexprType(t *testing.T) {
	td := &TypeDecl{
		Name: "Point",
		Fields: []Field{
			{Name: "x", Type: "int"},
			{Name: "y", Type: "int"},
		},
	}
	got := sexprType(td)
	want := "(type Point (fields (f x int) (f y int)))"
	if got != want {
		t.Fatalf("sexprType = %q, want %q", got, want)
	}
}
