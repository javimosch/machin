package main

import (
	"strings"
	"testing"
)

// BuildWasm must reject a program with no `export func` before ever invoking
// the zig/cc toolchain — this is the only branch of BuildWasm testable
// without a wasm32-wasi cross-compiler installed in CI.
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
