package main

import (
	"os"
	"path/filepath"
	"testing"
)

// cmdLexTest had no direct test coverage; cover its error paths: missing argument, file not found,
// and a successful lex of a simple source file.
func TestCmdLexTestMissingArg(t *testing.T) {
	if err := cmdLexTest(nil); err == nil {
		t.Fatal("expected error for missing file arg, got nil")
	}
}

func TestCmdLexTestFileNotFound(t *testing.T) {
	if err := cmdLexTest([]string{"/nonexistent/path/file.mfl"}); err == nil {
		t.Fatal("expected error for file not found, got nil")
	}
}

func TestCmdLexTestSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mfl")
	src := "func main(){x:=1}"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdLexTest([]string{path}); err != nil {
		t.Fatalf("cmdLexTest() error = %v", err)
	}
}

// cmdLexBench had no direct test coverage; cover its error paths: missing arguments, file not found.
func TestCmdLexBenchMissingArgs(t *testing.T) {
	if err := cmdLexBench(nil); err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if err := cmdLexBench([]string{"file.mfl"}); err == nil {
		t.Fatal("expected error for missing iters arg, got nil")
	}
}

func TestCmdLexBenchFileNotFound(t *testing.T) {
	if err := cmdLexBench([]string{"/nonexistent/path/file.mfl", "10"}); err == nil {
		t.Fatal("expected error for file not found, got nil")
	}
}

func TestCmdLexBenchSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mfl")
	src := "func main(){x:=1}"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdLexBench([]string{path, "3"}); err != nil {
		t.Fatalf("cmdLexBench() error = %v", err)
	}
}
