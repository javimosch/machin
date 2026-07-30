package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stat/is_dir/file_size/is_symlink are the metadata half of file I/O. MFL could
// list a directory and read a file, but could not ask what an entry IS -- which
// made a recursive walk impossible to write safely, since every list_dir() entry
// may be a subdirectory.
func TestStatKindSizeAndMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	q := func(p string) string { return `"` + filepath.Join(dir, p) + `"` }
	src := `func main() {
    k := 0
    sz := 0
    mt := 0
    k, sz, mt = stat(` + q("f.txt") + `)
    println("file=" + str(k) + "," + str(sz) + "," + str(mt > 0))
    k, sz, mt = stat(` + q("sub") + `)
    println("dir=" + str(k))
    k, sz, mt = stat(` + q("nope") + `)
    println("missing=" + str(k) + "," + str(sz))
}`
	out := runNative(t, src)
	for _, want := range []string{"file=1,6,true", "dir=2", "missing=0,-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stat output = %q, want it to contain %q", out, want)
		}
	}
}

func TestIsDirAndFileSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	q := func(p string) string { return `"` + filepath.Join(dir, p) + `"` }
	src := `func main() {
    println("d_dir=" + str(is_dir(` + q("sub") + `)))
    println("f_dir=" + str(is_dir(` + q("f.txt") + `)))
    println("x_dir=" + str(is_dir(` + q("nope") + `)))
    println("size=" + str(file_size(` + q("f.txt") + `)))
    println("nosize=" + str(file_size(` + q("nope") + `)))
}`
	out := runNative(t, src)
	for _, want := range []string{"d_dir=true", "f_dir=false", "x_dir=false", "size=6", "nosize=-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("is_dir/file_size output = %q, want it to contain %q", out, want)
		}
	}
}

// The distinction that makes a walk terminate: a symlink TO a directory is
// is_dir (stat follows) AND is_symlink (lstat does not). A walker that only had
// is_dir would recurse into a link pointing back up the tree, forever.
func TestIsSymlinkDistinguishesLinkToDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "sub"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	q := func(p string) string { return `"` + filepath.Join(dir, p) + `"` }
	src := `func main() {
    println("link_isdir=" + str(is_dir(` + q("link") + `)))
    println("link_islink=" + str(is_symlink(` + q("link") + `)))
    println("sub_islink=" + str(is_symlink(` + q("sub") + `)))
}`
	out := runNative(t, src)
	for _, want := range []string{"link_isdir=true", "link_islink=true", "sub_islink=false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("is_symlink output = %q, want it to contain %q", out, want)
		}
	}
}

// End-to-end: the motivating program. A recursive walk over a tree containing a
// symlink cycle must terminate and report the same totals as the real files.
func TestRecursiveWalkTerminatesOverSymlinkCycle(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("678"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a link pointing back at the root: a following walk never terminates
	if err := os.Symlink(dir, filepath.Join(dir, "sub", "cycle")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	walkFn := `func walk(d) (n, b) {
    entries := list_dir(d)
    i := 0
    for i < len(entries) {
        p := d + "/" + entries[i]
        if !is_symlink(p) {
            if is_dir(p) {
                sn := 0
                sb := 0
                sn, sb = walk(p)
                n = n + sn
                b = b + sb
            } else {
                n = n + 1
                b = b + file_size(p)
            }
        }
        i = i + 1
    }
}`
	mainFn := `func main() {
    f := 0
    t := 0
    f, t = walk("` + dir + `")
    println("files=" + str(f) + " bytes=" + str(t))
}`
	out := runNative(t, walkFn, mainFn)
	if !strings.Contains(out, "files=2 bytes=8") {
		t.Fatalf("walk output = %q, want it to contain %q", out, "files=2 bytes=8")
	}
}
