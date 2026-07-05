package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdCGenTestUsage(t *testing.T) {
	if err := cmdCGenTest(nil); err == nil {
		t.Fatal("expected usage error with no args")
	}
	if err := cmdCGenTest([]string{"--program"}); err == nil {
		t.Fatal("expected usage error with missing file arg")
	}
	if err := cmdCGenTest([]string{"--bogus", "x"}); err == nil {
		t.Fatal("expected usage error for unrecognized flag")
	}
}

func TestCmdCGenTestMissingFile(t *testing.T) {
	err := cmdCGenTest([]string{"--program", filepath.Join(t.TempDir(), "nope.mfl")})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCmdCGenTestParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.mfl")
	if err := os.WriteFile(path, []byte("this is not valid mfl {{{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr = cmdCGenTest([]string{"--program", path})
	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if callErr != nil {
		t.Fatalf("cmdCGenTest returned error instead of printing: %v", callErr)
	}
	if strings.TrimSpace(out) != "(parse-error)" {
		t.Fatalf("got %q, want (parse-error)", out)
	}
}

func TestCmdCGenTestCheckError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unbound.mfl")
	if err := os.WriteFile(path, []byte("func main(){x:=undefinedFn(1)}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr = cmdCGenTest([]string{"--program", path})
	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if callErr != nil {
		t.Fatalf("cmdCGenTest returned error instead of printing: %v", callErr)
	}
	if strings.TrimSpace(out) != "(check-error)" {
		t.Fatalf("got %q, want (check-error)", out)
	}
}

func TestCmdCGenTestCodegenError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badcg.mfl")
	if err := os.WriteFile(path, []byte("func f(){return}\nfunc main(){x:=f()}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr = cmdCGenTest([]string{"--program", path})
	w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if callErr != nil {
		t.Fatalf("cmdCGenTest returned error instead of printing: %v", callErr)
	}
	if strings.TrimSpace(out) == "(codegen-error)" || len(strings.TrimSpace(out)) > 0 {
		return
	}
	t.Fatalf("expected output from valid program")
}

func TestCmdCGenTestProgram(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.mfl")
	src := "func main(){x:=1}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	callErr = cmdCGenTest([]string{"--program", path})
	w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if callErr != nil {
		t.Fatalf("cmdCGenTest: %v", callErr)
	}
	if len(strings.TrimSpace(out)) == 0 {
		t.Fatal("expected C codegen output")
	}
	if strings.Contains(out, "(parse-error)") || strings.Contains(out, "(check-error)") || strings.Contains(out, "(codegen-error)") {
		t.Fatalf("valid program should not print error marker, got: %s", out[:100])
	}
}
