package main

import "testing"

func TestParseGlobalSimple(t *testing.T) {
	gv, err := ParseGlobal(`var counter = 0`)
	if err != nil {
		t.Fatalf("ParseGlobal: unexpected error: %v", err)
	}
	if gv.Name != "counter" {
		t.Errorf("gv.Name = %q, want %q", gv.Name, "counter")
	}
	lit, ok := gv.Init.(*IntLit)
	if !ok || lit.Val != 0 {
		t.Errorf("gv.Init = %#v, want *IntLit{Val: 0}", gv.Init)
	}
}

func TestParseGlobalWithStructLiteral(t *testing.T) {
	structs := map[string]bool{"Config": true}
	gv, err := ParseGlobalWith(`var cfg = Config{name: "x"}`, structs)
	if err != nil {
		t.Fatalf("ParseGlobalWith: unexpected error: %v", err)
	}
	if gv.Name != "cfg" {
		t.Errorf("gv.Name = %q, want %q", gv.Name, "cfg")
	}
	sl, ok := gv.Init.(*StructLit)
	if !ok {
		t.Fatalf("gv.Init = %T, want *StructLit", gv.Init)
	}
	if sl.Type != "Config" {
		t.Errorf("sl.Type = %q, want %q", sl.Type, "Config")
	}
}

func TestParseGlobalWithoutStructsUnknownTypeNotLiteral(t *testing.T) {
	// Without "Config" registered as a known struct name, `Config{...}` cannot be
	// parsed as a composite literal, so this must fail rather than silently
	// misparse.
	if _, err := ParseGlobalWith(`var cfg = Config{name: "x"}`, nil); err == nil {
		t.Errorf("ParseGlobalWith: want error when struct type is unregistered, got nil")
	}
}

func TestParseGlobalMissingInitializer(t *testing.T) {
	if _, err := ParseGlobal(`var counter`); err == nil {
		t.Errorf("ParseGlobal: want error for missing `= <expr>`, got nil")
	}
}

func TestParseGlobalTrailingTokens(t *testing.T) {
	if _, err := ParseGlobal(`var counter = 0 extra`); err == nil {
		t.Errorf("ParseGlobal: want error for trailing tokens after the initializer, got nil")
	}
}

func TestParseGlobalMissingVarKeyword(t *testing.T) {
	if _, err := ParseGlobal(`counter = 0`); err == nil {
		t.Errorf("ParseGlobal: want error when leading `var` keyword is missing, got nil")
	}
}

func TestParseGlobalMissingName(t *testing.T) {
	if _, err := ParseGlobal(`var = 0`); err == nil {
		t.Errorf("ParseGlobal: want error when the global has no name, got nil")
	}
}
