package main

import (
	"strings"
	"testing"
)

// ptr_str reads a NUL-terminated string out of raw memory — the host->wasm string
// direction. Poke bytes into an alloc'd buffer, then read them back as a string.
func TestPtrStrRoundTrip(t *testing.T) {
	src := `func main() {
		p := alloc(4)
		poke_u8(p, 0, 72)  poke_u8(p, 1, 105)  poke_u8(p, 2, 0)
		s := ptr_str(p)
		println(s + "/" + str(len(s)))
		free(p)
	}`
	fn, err := ParseFunc(normalize(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(&Program{Funcs: []*FuncDecl{fn}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "Hi/2" {
		t.Fatalf("ptr_str round-trip = %q, want %q", strings.TrimSpace(out), "Hi/2")
	}
}

// ptr_str returns a string; passing it where a string is needed type-checks, and
// its argument is an int pointer.
func TestPtrStrTypes(t *testing.T) {
	fn, _ := ParseFunc(normalize(`func main() { s := ptr_str(alloc(2))  println(s) }`))
	if _, err := Check(&Program{Funcs: []*FuncDecl{fn}}); err != nil {
		t.Fatalf("ptr_str should type-check (int -> string): %v", err)
	}
	bad, _ := ParseFunc(normalize(`func main() { println(ptr_str("not a pointer")) }`))
	if _, err := Check(&Program{Funcs: []*FuncDecl{bad}}); err == nil {
		t.Fatal("ptr_str on a string arg should be a type error")
	}
}

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
