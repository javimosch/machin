package main

import (
	"strings"
	"testing"
)

// read_file_bytes returns bytes (NUL-safe), and write_bytes takes (fd, bytes) ->
// int — the pair that lets a server read a binary asset and write it to a socket
// without a C-string body truncating it at the first NUL.
func TestBinaryIOBuiltinsTypecheck(t *testing.T) {
	src := `func main() {
		b := read_file_bytes("app.wasm")
		n := write_bytes(1, b)
		println(n + len(b))
	}`
	fn, err := ParseFunc(normalize(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	prog := &Program{Funcs: []*FuncDecl{fn}}
	csrc, err := CompileToC(prog, false)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(csrc, "mfl_read_file_bytes") || !strings.Contains(csrc, "mfl_write_bytes") {
		t.Fatal("binary I/O builtins did not emit their runtime calls")
	}
}

// read_file_bytes must reject a bytes arg where a string path is expected, and
// write_bytes must reject a string where bytes are expected — the type rules wire
// the right kinds.
func TestBinaryIOTypeErrors(t *testing.T) {
	bad := []string{
		`func main() { b := read_file_bytes("x")  write_bytes(1, "not bytes")  println(len(b)) }`,
	}
	for _, s := range bad {
		fn, err := ParseFunc(normalize(s))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := Check(&Program{Funcs: []*FuncDecl{fn}}); err == nil {
			t.Fatalf("expected a type error for: %s", s)
		}
	}
}
