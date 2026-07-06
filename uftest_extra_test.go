package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdUFTestFuncNoParams covers the "func -|R" form (no params, just a
// return slot), which the existing "func 0,1|2" test never exercises.
func TestCmdUFTestFuncNoParams(t *testing.T) {
	script := "int\nfunc -|0\ndump\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "script.uf")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdUFTest([]string{path}); err != nil {
		t.Fatalf("cmdUFTest() error = %v", err)
	}
}

// TestCmdUFTestSkipsAfterFailure covers the "once a union mismatch is hit,
// every remaining line except dump is skipped" branch: the trailing "var"
// and "int" ops here must not add new slots to the table.
func TestCmdUFTestSkipsAfterFailure(t *testing.T) {
	script := "int\nstring\nunion 0 1\nvar\nint\ndump\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "script.uf")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = cmdUFTest([]string{path})
	w.Close()
	os.Stdout = stdout
	if err != nil {
		t.Fatalf("cmdUFTest() error = %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if !strings.Contains(out, "err=1") {
		t.Fatalf("expected err=1 in output, got:\n%s", out)
	}
	// Only the two original slots (int, string) should appear; the "var"
	// and "int" ops issued after the failure must have been skipped.
	if strings.Count(out, "root=") != 2 {
		t.Fatalf("expected exactly 2 slots after skipped post-failure ops, got:\n%s", out)
	}
}
