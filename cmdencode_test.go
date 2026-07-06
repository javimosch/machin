package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdEncodeNoArgs(t *testing.T) {
	err := cmdEncode(nil)
	if err == nil {
		t.Fatal("expected error for missing source file, got nil")
	}
	want := "encode: need at least one source file"
	if err.Error() != want {
		t.Errorf("cmdEncode(nil) error = %q, want %q", err.Error(), want)
	}
}

func TestComposeSourcesEmpty(t *testing.T) {
	_, _, err := composeSources([]string{})
	if err == nil {
		t.Fatal("expected error for empty sources list, got nil")
	}
}

func TestComposeSourcesMissingFile(t *testing.T) {
	_, _, err := composeSources([]string{"does-not-exist.mfl"})
	if err == nil {
		t.Fatal("expected error for nonexistent source file, got nil")
	}
}

func TestCmdEncodeValidSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mfl")
	src := "func main() { println(42) }"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = cmdEncode([]string{path})
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("cmdEncode: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))
	if !strings.Contains(output, "func main()") {
		t.Fatalf("encoded output should contain normalized function, got %q", output)
	}
}
