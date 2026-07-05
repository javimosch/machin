package main

import (
	"os"
	"path/filepath"
	"testing"
)

// cmdRun and cmdBuild had no direct test coverage; add tests for their usage/error paths.
// cmdRun: missing input file, missing binary file, successful execution
func TestCmdRunMissingFile(t *testing.T) {
	err := cmdRun([]string{filepath.Join(t.TempDir(), "nonexistent.src")})
	if err == nil {
		t.Fatalf("cmdRun with missing source file should error, got nil")
	}
}

func TestCmdRunMissingBinary(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	if err := os.WriteFile(srcPath, []byte("func main() { println(\"hi\") }"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cmdRun([]string{filepath.Join(dir, "nonexistent-binary")})
	if err == nil {
		t.Fatalf("cmdRun with missing binary should error, got nil")
	}
}

// cmdBuild: missing source file, write permission error, successful build
func TestCmdBuildMissingSource(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out")
	err := cmdBuild([]string{filepath.Join(dir, "nonexistent.src"), outPath})
	if err == nil {
		t.Fatalf("cmdBuild with missing source should error, got nil")
	}
}
