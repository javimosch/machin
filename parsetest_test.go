package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

// cmdParseTest had no direct test coverage; cover its subcommands and error paths.
func TestCmdParseTestMissingSubcommand(t *testing.T) {
	if err := cmdParseTest(nil); err == nil {
		t.Fatal("expected error for missing subcommand, got nil")
	}
}

func TestCmdParseTestExprMissingArg(t *testing.T) {
	if err := cmdParseTest([]string{"--expr"}); err == nil {
		t.Fatal("expected error for --expr missing source arg, got nil")
	}
}

func TestCmdParseTestExprSuccess(t *testing.T) {
	if err := cmdParseTest([]string{"--expr", "1"}); err != nil {
		t.Fatalf("cmdParseTest --expr: %v", err)
	}
}

func TestCmdParseTestExprTrailingTokens(t *testing.T) {
	if err := cmdParseTest([]string{"--expr", "1 2"}); err == nil {
		t.Fatal("expected error for trailing tokens after expression, got nil")
	}
}

func TestCmdParseTestFuncMissingArg(t *testing.T) {
	if err := cmdParseTest([]string{"--func"}); err == nil {
		t.Fatal("expected error for --func missing source arg, got nil")
	}
}

func TestCmdParseTestFuncSuccess(t *testing.T) {
	if err := cmdParseTest([]string{"--func", "func main(){x:=1}"}); err != nil {
		t.Fatalf("cmdParseTest --func: %v", err)
	}
}

func TestCmdParseTestProgramMissingArg(t *testing.T) {
	if err := cmdParseTest([]string{"--program"}); err == nil {
		t.Fatal("expected error for --program missing file arg, got nil")
	}
}

func TestCmdParseTestProgramFileNotFound(t *testing.T) {
	if err := cmdParseTest([]string{"--program", "/nonexistent/path/file.mfl"}); err == nil {
		t.Fatal("expected error for --program file not found, got nil")
	}
}

func TestCmdParseTestProgramSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.mfl")
	src := "func main(){x:=1}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdParseTest([]string{"--program", path}); err != nil {
		t.Fatalf("cmdParseTest --program: %v", err)
	}
}
