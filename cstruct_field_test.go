package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Compile a whole .src (externs and type declarations included, which
// runNative's per-function parser cannot take) and run it.
func buildAndRunSrc(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "p.src")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mflPath := filepath.Join(dir, "p.mfl")
	out, err := exec.Command("go", "run", ".", "encode", srcPath).Output()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := os.WriteFile(mflPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "p")
	if b, err := exec.Command("go", "run", ".", "build", mflPath, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, b)
	}
	got, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, got)
	}
	return string(got)
}

// A user struct may hold a cstruct as a field. The mfl_ wrappers for cstructs
// land in the type list AFTER the user types whatever the source order says,
// so the typedef for the holder was emitted before the typedef for the field's
// type: gcc reported `unknown type name 'mfl_Pt'` and a cascade of int
// mismatches from its own error recovery, while `machin check` had already
// reported the program as fine. Header-less on purpose, so this needs no
// third-party library installed.
func TestCStructAsStructField(t *testing.T) {
	got := buildAndRunSrc(t, `extern "geom" {
	cstruct Pt { x f64 y f64 }
	fn geom_unused(Pt) f64
}

type Holder struct {
	p    Pt
	name string
}

func main() {
	h := Holder{}
	h.p = Pt{3.0, 4.0}
	h.name = "ok"
	println(h.name)
	println(str(h.p.x + h.p.y))
}
`)
	if want := "ok\n7\n"; got != want {
		t.Fatalf("cstruct as a struct field: got %q, want %q", got, want)
	}
}

// The same one level deeper: the ordering has to hold for a whole chain.
func TestCStructNestedTwoDeep(t *testing.T) {
	got := buildAndRunSrc(t, `extern "geom" {
	cstruct Pt { x f64 y f64 }
	fn geom_unused(Pt) f64
}

type Inner struct { p Pt }
type Outer struct { in Inner }

func main() {
	o := Outer{}
	o.in = Inner{}
	o.in.p = Pt{1.5, 2.5}
	println(str(o.in.p.x * o.in.p.y))
}
`)
	if want := "3.75\n"; !strings.HasPrefix(got, want) {
		t.Fatalf("cstruct two levels deep: got %q, want prefix %q", got, want)
	}
}
