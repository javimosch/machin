package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fsync is the missing half of durability: write_file returns once the data is
// in the page cache, so a power loss can still lose a "written" file. These
// tests cover the three cases a durable-write recipe depends on — a file, a
// DIRECTORY (creating a file is a directory change; without syncing it a fully
// synced file can still vanish), and a path that does not exist.
func TestFsyncFileDirAndMissing(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "d")
	src := `func main() {
    mkdir(` + `"` + sub + `"` + `)
    write_file(` + `"` + filepath.Join(sub, "a.txt") + `"` + `, "durable")
    println("file=" + str(fsync(` + `"` + filepath.Join(sub, "a.txt") + `"` + `)))
    println("dir=" + str(fsync(` + `"` + sub + `"` + `)))
    println("missing=" + str(fsync(` + `"` + filepath.Join(dir, "nope") + `"` + `)))
}`
	out := runNative(t, src)
	for _, want := range []string{"file=0", "dir=0", "missing=-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("fsync output = %q, want it to contain %q", out, want)
		}
	}
}

// The durable-write recipe must round-trip: the point of fsync is that the file
// is still readable and correct afterwards, not merely that it returns 0.
func TestFsyncPreservesContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "chunk.grg")
	src := `func main() {
    write_file(` + `"` + p + `"` + `, "record|1\nrecord|2")
    fsync(` + `"` + p + `"` + `)
    fsync(` + `"` + dir + `"` + `)
    println(read_file(` + `"` + p + `"` + `))
}`
	if out := runNative(t, src); !strings.Contains(out, "record|1") || !strings.Contains(out, "record|2") {
		t.Fatalf("content after fsync = %q, want both records", out)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "record|1\nrecord|2" {
		t.Fatalf("on-disk content = %q err=%v", b, err)
	}
}
