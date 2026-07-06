package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdPack had no direct test coverage; these cover its two error paths and the
// happy path where each non-blank line becomes one base64-encoded declaration.
func TestCmdPackWrongArgCount(t *testing.T) {
	if err := cmdPack(nil); err == nil {
		t.Fatal("expected error with zero args")
	}
	if err := cmdPack([]string{"a.mfl", "b.mfl"}); err == nil {
		t.Fatal("expected error with two args")
	}
}

func TestCmdPackMissingFile(t *testing.T) {
	if err := cmdPack([]string{filepath.Join(t.TempDir(), "nope.mfl")}); err == nil {
		t.Fatal("expected error for a missing file")
	}
}

func TestCmdPackEncodesEachDecl(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.mfl")
	decl := `func main() int { return 0 }`
	src := "\n" + decl + "\n\n" // blank lines around the decl must be skipped
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = cmdPack([]string{path})
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("cmdPack: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := strings.TrimSpace(string(buf[:n]))
	decoded, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatalf("output is not valid base64: %v", err)
	}
	if string(decoded) != decl {
		t.Fatalf("got decoded %q, want %q", decoded, decl)
	}
}

// declFromLine must tolerate input that is already packed (base64), so pack
// can safely be re-run on a file that mixes plain and packed lines.
func TestCmdPackTolerantOfAlreadyPackedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.mfl")
	decl := `func f() int { return 1 }`
	packed := base64.StdEncoding.EncodeToString([]byte(decl))
	if err := os.WriteFile(path, []byte(packed+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	err := cmdPack([]string{path})
	os.Stdout = old
	if devnull != nil {
		devnull.Close()
	}
	if err != nil {
		t.Fatalf("cmdPack should tolerate an already-packed line, got: %v", err)
	}
}

// TestCmdPackEmptyFile verifies pack handles a file with no declarations gracefully.
func TestCmdPackEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.mfl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Stdout
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	err := cmdPack([]string{path})
	os.Stdout = old
	if devnull != nil {
		devnull.Close()
	}
	if err != nil {
		t.Fatalf("cmdPack on an empty file should be a graceful no-op, got: %v", err)
	}
}
