package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFFIMath exercises Phase-1 C FFI end to end: an extern declaration with a
// header + link, a direct call to the foreign function, and linking via -lm.
func TestFFIMath(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "m" { header "math.h" link "m" fn sqrt(float) float fn pow(float, float) float }`,
		`func main(){ println(sqrt(2.0)) println(pow(2.0, 10.0)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := "1.41421\n1024\n"; out != want {
		t.Fatalf("FFI math: got %q, want %q", out, want)
	}
}

// A foreign call with the wrong argument count is a clean MFL type error, not a
// leaked cc failure.
func TestFFIArgCountChecked(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "m" { header "math.h" link "m" fn sqrt(float) float }`,
		`func main(){ println(sqrt(1.0, 2.0)) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "expected 1 args") {
		t.Fatalf("expected an arg-count error, got %v", err)
	}
}

// TestFFIStructReturn marshals a C struct returned by value: libc's
// div_t div(int, int) (no external library needed).
func TestFFIStructReturn(t *testing.T) {
	prog, err := ParseProgram([]string{
		`extern "c" { header "stdlib.h" cstruct div_t { quot i32  rem i32 } fn div(i32, i32) div_t }`,
		`func main(){ r := div(17, 5) println(r.quot, r.rem) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "3 2\n" {
		t.Fatalf("struct return: got %q, want \"3 2\\n\"", out)
	}
}

// TestFFIStructPass marshals an MFL struct into a C struct passed by value (and
// back). It compiles against a header written into a temp dir via `cflags -I`.
func TestFFIStructPass(t *testing.T) {
	dir := t.TempDir()
	hdr := `typedef struct { int x; int y; } Pt;
static int pt_sum(Pt p){ return p.x + p.y; }
static Pt pt_make(int x, int y){ Pt p; p.x = x; p.y = y; return p; }
`
	if err := os.WriteFile(filepath.Join(dir, "pt.h"), []byte(hdr), 0o644); err != nil {
		t.Fatal(err)
	}
	prog, err := ParseProgram([]string{
		`extern "pt" { cflags "-I` + dir + `" header "pt.h" cstruct Pt { x i32  y i32 } fn pt_sum(Pt) i32 fn pt_make(i32, i32) Pt }`,
		`func main(){ p := pt_make(3, 4) println(pt_sum(p)) println(pt_sum(Pt{10, 20})) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "7\n30\n" {
		t.Fatalf("struct pass: got %q, want \"7\\n30\\n\"", out)
	}
}

// TestFFIPointerHandle round-trips an opaque C handle (a FILE*) through MFL as a
// `ptr`: open, write, close — then Go verifies the file C actually wrote.
func TestFFIPointerHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	prog, err := ParseProgram([]string{
		`extern "c" { header "stdio.h" fn fopen(string, string) ptr fn fputs(string, ptr) i32 fn fclose(ptr) i32 }`,
		`func main(){ f := fopen("` + path + `", "w") fputs("via void* handle", f) fclose(f) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := RunCaptured(prog); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "via void* handle" {
		t.Fatalf("opaque handle round-trip: file has %q", got)
	}
}
