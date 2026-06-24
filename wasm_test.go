package main

import (
	"strings"
	"testing"
)

// compileWasm is a tiny helper: encode loose source, parse, and compile for the
// wasm target, returning the emitted C and the exported names.
func compileWasm(t *testing.T, srcs ...string) (string, []string) {
	t.Helper()
	var decls []string
	for _, s := range srcs {
		blocks, err := splitFunctions(s)
		if err != nil {
			t.Fatalf("split: %v", err)
		}
		for _, b := range blocks {
			decls = append(decls, normalize(b))
		}
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	csrc, exports, err := CompileToCTarget(prog, false, targetWasm)
	if err != nil {
		t.Fatalf("compile wasm: %v", err)
	}
	return csrc, exports
}

const wasmApp = `
extern "env" {
    fn set_html(string)
}
func view(n) (s) { s = "n=" + str(n * n) }
export func render(n) { set_html(view(n)) }
`

// A wasm module needs no main: an export func is its own reachability root, and
// the host drives the module through it. The native C entry point is omitted.
func TestWasmExportRootsWithoutMain(t *testing.T) {
	csrc, exports := compileWasm(t, wasmApp)
	if len(exports) != 1 || exports[0] != "render" {
		t.Fatalf("exports = %v, want [render]", exports)
	}
	if !strings.Contains(csrc, "mfl_render") {
		t.Fatal("render was tree-shaken despite being exported")
	}
	if !strings.Contains(csrc, "mfl_view") {
		t.Fatal("view (reachable from the export) was not emitted")
	}
	if strings.Contains(csrc, "int main(int argc") {
		t.Fatal("wasm target should not emit a native int main entry point")
	}
}

// FFI imports become wasm imports (import_module/import_name) so the host (JS)
// supplies them; exports carry export_name so the host sees the clean name.
func TestWasmImportAndExportAttributes(t *testing.T) {
	csrc, _ := compileWasm(t, wasmApp)
	if !strings.Contains(csrc, `import_module("env")`) || !strings.Contains(csrc, `import_name("set_html")`) {
		t.Fatal("FFI extern was not tagged as a wasm import")
	}
	if !strings.Contains(csrc, `export_name("render")`) {
		t.Fatal("exported function was not tagged with export_name")
	}
}

// The POSIX socket/tty runtime is pay-as-you-go under wasm: a frontend module
// that touches neither pulls in no socket()/termios symbols.
func TestWasmOmitsUnusedPosixRuntime(t *testing.T) {
	csrc, _ := compileWasm(t, wasmApp)
	for _, sym := range []string{"socket(", "SO_REUSEADDR", "mfl_listen", "mfl_dial", "tcsetattr", "mfl_raw_mode"} {
		if strings.Contains(csrc, sym) {
			t.Fatalf("wasm module pulled in unused POSIX symbol %q", sym)
		}
	}
}

// But when the program does use sockets, the net runtime is emitted (so a wasm
// program that genuinely dials is still well-formed C).
func TestWasmKeepsUsedNetRuntime(t *testing.T) {
	csrc, _ := compileWasm(t, `
extern "env" { fn log_i(i32) }
export func ping() { fd := dial("host", 80)  log_i(int(fd)) }
`)
	if !strings.Contains(csrc, "mfl_dial") {
		t.Fatal("net runtime omitted despite dial() being called")
	}
}

// The native target is unchanged: it always carries the POSIX runtime and emits
// the int main entry point, and FFI externs stay plain (no wasm attributes).
func TestNativeTargetUnchanged(t *testing.T) {
	prog, err := ParseProgram([]string{normalize("func main() { println(1) }")})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	csrc, exports, err := CompileToCTarget(prog, false, targetNative)
	if err != nil {
		t.Fatalf("compile native: %v", err)
	}
	if len(exports) != 0 {
		t.Fatalf("native exports = %v, want none", exports)
	}
	if !strings.Contains(csrc, "mfl_listen") || !strings.Contains(csrc, "mfl_raw_mode") {
		t.Fatal("native target should always carry the full POSIX runtime")
	}
	if !strings.Contains(csrc, "int main(int argc") {
		t.Fatal("native target must emit int main")
	}
	if strings.Contains(csrc, "import_module") {
		t.Fatal("native target must not emit wasm import attributes")
	}
}
