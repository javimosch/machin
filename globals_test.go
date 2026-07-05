package main

import (
	"strings"
	"testing"
)

// progFromSrc encodes loose source the way `machin encode` does, then parses.
func progFromSrc(t *testing.T, src string) *Program {
	t.Helper()
	blocks, err := splitFunctions(src)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var decls []string
	for _, b := range blocks {
		decls = append(decls, normalize(b))
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

// A package global holds state across calls: bump accumulates into it.
func TestGlobalStatePersists(t *testing.T) {
	prog := progFromSrc(t, `
var count = 0
func bump(d) (n) { count = count + d  n = count }
func main() { println(bump(2))  println(bump(3))  println(bump(0)) }
`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Fields(out); len(got) != 3 || got[0] != "2" || got[1] != "5" || got[2] != "5" {
		t.Fatalf("global did not accumulate: %q (want 2 5 5)", out)
	}
}

// `splitFunctions` must keep a brace-less top-level `var` as its own declaration
// (not merge it into the following function).
func TestGlobalSplitsStandalone(t *testing.T) {
	prog := progFromSrc(t, "var a = 1\nvar b = 2\nfunc main() { println(a + b) }")
	if len(prog.Globals) != 2 {
		t.Fatalf("got %d globals, want 2", len(prog.Globals))
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "3" {
		t.Fatalf("globals not summed: %q", out)
	}
}

// A `:=` inside a function shadows a global of the same name; the global is
// untouched. An `=` assigns the global.
func TestGlobalShadowing(t *testing.T) {
	prog := progFromSrc(t, `
var x = 100
func shadow() (r) { x := 7  r = x }
func global() (r) { r = x }
func main() { println(shadow())  println(global()) }
`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Fields(out); len(got) != 2 || got[0] != "7" || got[1] != "100" {
		t.Fatalf("shadowing wrong: %q (want 7 100)", out)
	}
}

// A global is emitted as a C static `mfl_g_<name>` plus a constructor that runs
// its initializer; references compile to that symbol.
func TestGlobalCodegenShape(t *testing.T) {
	prog := progFromSrc(t, "var count = 0\nfunc main() { count = count + 1  println(count) }")
	csrc, err := CompileToC(prog, false)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"mfl_g_count", "__attribute__((constructor))", "mfl_g_count = "} {
		if !strings.Contains(csrc, want) {
			t.Fatalf("emitted C missing %q", want)
		}
	}
}

// Globals close the loop for the wasm target: an exported function can hold state
// between host calls (the global is a module static, initialized at _initialize).
func TestGlobalUnderWasmTarget(t *testing.T) {
	prog := progFromSrc(t, `
var count = 0
export func bump(d) (n) { count = count + d  n = count }
`)
	csrc, _, err := CompileToCTarget(prog, false, targetWasm)
	if err != nil {
		t.Fatalf("compile wasm: %v", err)
	}
	if !strings.Contains(csrc, "mfl_g_count") || !strings.Contains(csrc, "__attribute__((constructor))") {
		t.Fatal("wasm target did not emit the global + its constructor")
	}
}

// Multiple globals maintain independent state across calls; each can be
// modified separately and both persist between function calls.
func TestMultipleGlobalsIndependent(t *testing.T) {
	prog := progFromSrc(t, `
var x = 1
var y = 2
func incBoth() { x = x + 1  y = y + 10 }
func main() { incBoth()  println(x)  println(y)  incBoth()  println(x)  println(y) }
`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Fields(out); len(got) != 4 || got[0] != "2" || got[1] != "12" || got[2] != "3" || got[3] != "22" {
		t.Fatalf("multiple globals failed: %q (want 2 12 3 22)", out)
	}
}
