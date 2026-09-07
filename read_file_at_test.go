package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read_file_at is the positional read, the mirror of write_file_at. It exists
// because pulling one range out of a file with read_file_bytes copies the WHOLE
// file: scanning a 264 MB torrent piece by piece read 279 GB and was OOM-killed.
func TestReadFileAtRanges(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.bin")
	if err := os.WriteFile(p, []byte("ABCDEFGHIJ"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := fmt.Sprintf(`func main() {
		p := %q
		println(bytes_str(read_file_at(p, 3, 4)))
		println(bytes_str(read_file_at(p, 0, 2)))
		println(str(len(read_file_at(p, 8, 99))))
		println(str(len(read_file_at(p, 50, 4))))
		println(str(len(read_file_at(p, 0 - 1, 4))))
		println(str(len(read_file_at(p, 0, 0))))
		println(str(len(read_file_at(p + ".missing", 0, 4))))
		println(str(len(read_file_at(%q, 0, 4))))
	}`, p, dir)
	fn, err := ParseFunc(normalize(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(&Program{Funcs: []*FuncDecl{fn}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// mid range, head, short read at EOF, past EOF, negative offset,
	// zero length, missing file, a directory
	want := "DEFG\nAB\n2\n0\n0\n0\n0\n0"
	if strings.TrimSpace(out) != want {
		t.Fatalf("read_file_at = %q, want %q", strings.TrimSpace(out), want)
	}
}

// It returns bytes and takes (string, int, int); the checker must agree.
func TestReadFileAtTypes(t *testing.T) {
	fn, _ := ParseFunc(normalize(`func main() { b := read_file_at("f", 0, 4)  println(str(len(b))) }`))
	if _, err := Check(&Program{Funcs: []*FuncDecl{fn}}); err != nil {
		t.Fatalf("read_file_at should type-check (string,int,int -> bytes): %v", err)
	}
	bad, _ := ParseFunc(normalize(`func main() { println(str(len(read_file_at("f", 0)))) }`))
	if _, err := Check(&Program{Funcs: []*FuncDecl{bad}}); err == nil {
		t.Fatal("read_file_at with 2 args should be rejected")
	}
}
