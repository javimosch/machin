package main

import "testing"

// isExternDirective checks if a word is an extern directive keyword
// (header, link, cflags, fn, cstruct); these cannot be used as return types.
func TestIsExternDirective(t *testing.T) {
	cases := []struct {
		word string
		want bool
	}{
		{"header", true},
		{"link", true},
		{"cflags", true},
		{"fn", true},
		{"cstruct", true},
		{"int", false},
		{"string", false},
		{"myType", false},
		{"", false},
		{"FN", false},     // case-sensitive
		{"Header", false}, // case-sensitive
	}
	for _, c := range cases {
		if got := isExternDirective(c.word); got != c.want {
			t.Errorf("isExternDirective(%q) = %v, want %v", c.word, got, c.want)
		}
	}
}

// ParseExtern's own Lex error path, and parseExternDecl's directive-argument
// error branches (header/link/cflags/cstruct each expect a specific token
// after the keyword), had zero test coverage.
func TestParseExternLexError(t *testing.T) {
	if _, err := ParseExtern(`extern "m { fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected a lex error for an unterminated string, got nil")
	}
}

func TestParseExternHeaderMissingString(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { header 123 fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for header directive missing a string, got nil")
	}
}

func TestParseExternLinkMissingString(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { link 123 fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for link directive missing a string, got nil")
	}
}

func TestParseExternCflagsMissingString(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { cflags 123 fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for cflags directive missing a string, got nil")
	}
}

func TestParseExternCstructMissingName(t *testing.T) {
	if _, err := ParseExtern(`extern "m" { cstruct 123 { } fn sqrt(float) float }`); err == nil {
		t.Fatal("ParseExtern: expected error for cstruct directive missing a name, got nil")
	}
}

// Issue #697: an extern block may declare only cstructs (types-only extern —
// e.g. sharing C layouts across modules, or types bound through a shim header).
func TestParseExternCstructOnly(t *testing.T) {
	ed, err := ParseExtern(`extern "raylib" { cstruct Foo { a f32 b f32 } }`)
	if err != nil {
		t.Fatalf("ParseExtern cstruct-only: %v", err)
	}
	if len(ed.Funcs) != 0 {
		t.Fatalf("expected no fns, got %d", len(ed.Funcs))
	}
	if len(ed.Structs) != 1 || ed.Structs[0].Name != "Foo" || len(ed.Structs[0].Fields) != 2 {
		t.Fatalf("unexpected cstructs: %+v", ed.Structs)
	}
}

// header/link/cflags are modifiers: a block with no fn and no cstruct still
// declares nothing foreign, so it stays an error (with an accurate message).
func TestParseExternNoDeclarations(t *testing.T) {
	for _, src := range []string{
		`extern "m" {}`,
		`extern "m" { header "math.h" link "m" cflags "-O2" }`,
	} {
		if _, err := ParseExtern(src); err == nil {
			t.Fatalf("ParseExtern(%q): expected error, got nil", src)
		}
	}
}
