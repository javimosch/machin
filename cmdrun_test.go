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

func TestCmdRunWithSafeFlag(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	if err := os.WriteFile(srcPath, []byte("func main() { println(\"safe\") }"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cmdRun --safe should build a memory-safe binary without error
	if err := cmdRun([]string{"--safe", srcPath}); err != nil {
		t.Fatalf("cmdRun --safe: %v", err)
	}
}

func TestCmdBuildEmitC(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	if err := os.WriteFile(srcPath, []byte("func main() { }"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.c")
	// cmdBuild --emit-c should output C code without error
	if err := cmdBuild([]string{"--emit-c", "-o", outPath, srcPath}); err != nil {
		t.Fatalf("cmdBuild --emit-c: %v", err)
	}
}

func TestCmdBuildInvalidTarget(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	if err := os.WriteFile(srcPath, []byte("func main() { }"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cmdBuild with unknown --target should error
	if err := cmdBuild([]string{"--target", "invalid", srcPath}); err == nil {
		t.Fatal("cmdBuild with invalid --target should error, got nil")
	}
}

func TestCmdBuildStaticWithWasm(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	if err := os.WriteFile(srcPath, []byte("func main() { }"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cmdBuild --static with wasm target should error
	if err := cmdBuild([]string{"--static", "--target", "wasm", srcPath}); err == nil {
		t.Fatal("cmdBuild --static with wasm should error, got nil")
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

func TestCmdRunWithRaceSafeFlag(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	// Simple sequential code that passes race checking
	if err := os.WriteFile(srcPath, []byte("func main() { x := 1; println(x) }"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cmdRun --race-safe should pass through raceGate and execute
	if err := cmdRun([]string{"--race-safe", srcPath}); err != nil {
		t.Fatalf("cmdRun --race-safe: %v", err)
	}
}

func TestCmdBuildWithRaceSafeFlag(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	outPath := filepath.Join(dir, "out")
	// Simple sequential code that passes race checking
	if err := os.WriteFile(srcPath, []byte("func main() { x := 1; println(x) }"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cmdBuild --race-safe should pass through raceGate and build successfully
	if err := cmdBuild([]string{"--race-safe", "-o", outPath, srcPath}); err != nil {
		t.Fatalf("cmdBuild --race-safe: %v", err)
	}
}

func TestCmdBuildMissingFlagArg(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "prog.src")
	if err := os.WriteFile(srcPath, []byte("func main() { }"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cmdBuild -o with no path should error
	if err := cmdBuild([]string{"-o", srcPath}); err == nil {
		t.Fatal("cmdBuild -o with no path should error, got nil")
	}
	// cmdBuild --target with no value should error
	if err := cmdBuild([]string{"--target", srcPath}); err == nil {
		t.Fatal("cmdBuild --target with no value should error, got nil")
	}
}
