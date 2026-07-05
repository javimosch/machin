package main

import "testing"

func TestStringZeroInits(t *testing.T) {
	// Test struct with string fields and other field types
	prog := &Program{
		Types: []*TypeDecl{
			{
				Name: "Point",
				Fields: []Field{
					{Name: "x", Type: "int"},
					{Name: "y", Type: "int"},
				},
			},
			{
				Name: "Person",
				Fields: []Field{
					{Name: "name", Type: "string"},
					{Name: "age", Type: "int"},
					{Name: "email", Type: "string"},
				},
			},
			{
				Name: "Nested",
				Fields: []Field{
					{Name: "id", Type: "string"},
					{Name: "data", Type: "Person"},
				},
			},
			{
				Name: "Mixed",
				Fields: []Field{
					{Name: "text", Type: "string"},
					{Name: "count", Type: "int"},
					{Name: "vals", Type: "[]string"},
					{Name: "lookup", Type: "map[string]int"},
					{Name: "ch", Type: "chan int"},
					{Name: "fn", Type: "func"},
				},
			},
		},
		Funcs: []*FuncDecl{
			{Name: "main", Params: []string{}, Returns: []string{}, Body: []Stmt{}},
		},
	}

	c, err := Check(prog)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	g := &cgen{c: c}

	tests := []struct {
		typeStr string
		want    []string
	}{
		{"Point", nil},                    // no string fields
		{"Person", []string{".f_name = \"\"", ".f_email = \"\""}},
		{"Nested", []string{".f_id = \"\"", ".f_data = (mfl_Person){.f_name = \"\", .f_email = \"\"}"}},
		{"Mixed", []string{".f_text = \"\""}},
		{"Unknown", nil}, // struct doesn't exist
	}

	for _, tt := range tests {
		got := g.stringZeroInits(tt.typeStr)
		if len(got) != len(tt.want) {
			t.Errorf("stringZeroInits(%q) got %d items, want %d", tt.typeStr, len(got), len(tt.want))
			t.Logf("  got: %v", got)
			t.Logf("  want: %v", tt.want)
			continue
		}
		for i, w := range tt.want {
			if i >= len(got) || got[i] != w {
				t.Errorf("stringZeroInits(%q)[%d] = %q, want %q", tt.typeStr, i, got[i], w)
			}
		}
	}
}

func TestStringZeroInitsEmpty(t *testing.T) {
	// Test struct with no fields
	prog := &Program{
		Types: []*TypeDecl{
			{Name: "Empty", Fields: []Field{}},
		},
		Funcs: []*FuncDecl{
			{Name: "main", Params: []string{}, Returns: []string{}, Body: []Stmt{}},
		},
	}

	c, err := Check(prog)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	g := &cgen{c: c}
	got := g.stringZeroInits("Empty")
	if len(got) != 0 {
		t.Errorf("stringZeroInits(Empty) = %v, want nil", got)
	}
}

func TestStringZeroInitsOnlyStrings(t *testing.T) {
	// Test struct with only string fields
	prog := &Program{
		Types: []*TypeDecl{
			{
				Name: "Strings",
				Fields: []Field{
					{Name: "a", Type: "string"},
					{Name: "b", Type: "string"},
					{Name: "c", Type: "string"},
				},
			},
		},
		Funcs: []*FuncDecl{
			{Name: "main", Params: []string{}, Returns: []string{}, Body: []Stmt{}},
		},
	}

	c, err := Check(prog)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	g := &cgen{c: c}
	got := g.stringZeroInits("Strings")
	if len(got) != 3 {
		t.Errorf("stringZeroInits(Strings) = %v, want 3 items", got)
	}
	want := []string{".f_a = \"\"", ".f_b = \"\"", ".f_c = \"\""}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Errorf("stringZeroInits(Strings)[%d] = %q, want %q", i, got[i], w)
		}
	}
}
