package main

import (
	"strings"
	"testing"
)

// BuildWasm must reject a program with no `export func` before ever invoking
// the zig/cc toolchain — this is one of two branches of BuildWasm testable
// without a wasm32-wasi cross-compiler installed in CI (the other being a
// CompileToCTarget failure, covered by TestBuildWasmCompileError below).
func TestBuildWasmNoExports(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { println("hi") }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = BuildWasm(prog, "/tmp/mfl-buildwasm-test-out.wasm", false)
	if err == nil {
		t.Fatal("BuildWasm: expected error for a program with no export func, got nil")
	}
	if !strings.Contains(err.Error(), "no exported functions") {
		t.Fatalf("BuildWasm error = %q, want it to mention no exported functions", err.Error())
	}
}

// A CompileToCTarget failure (here, a call to an undefined function) must
// surface from BuildWasm before it ever shells out to zig/cc.
func TestBuildWasmCompileError(t *testing.T) {
	prog, err := ParseProgram([]string{
		`export func main() { undefinedFunc() }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = BuildWasm(prog, "/tmp/mfl-buildwasm-test-compile-error.wasm", false)
	if err == nil {
		t.Fatal("BuildWasm: expected an error for a call to an undefined function, got nil")
	}
	if !strings.Contains(err.Error(), "undefined function") {
		t.Fatalf("BuildWasm error = %q, want it to mention the undefined function", err.Error())
	}
}
