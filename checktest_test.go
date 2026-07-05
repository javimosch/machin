package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdCheckTest had no direct test coverage; cover its usage-error path plus
// the --program happy path, parse-error path, and check-error path.
func TestCmdCheckTestUsage(t *testing.T) {
	if err := cmdCheckTest(nil); err == nil {
		t.Fatal("expected usage error with no args")
	}
	if err := cmdCheckTest([]string{"--program"}); err == nil {
		t.Fatal("expected usage error with missing file arg")
	}
	if err := cmdCheckTest([]string{"--bogus", "x"}); err == nil {
		t.Fatal("expected usage error for unrecognized flag")
	}
}

func TestCmdCheckTestMissingFile(t *testing.T) {
	err := cmdCheckTest([]string{"--program", filepath.Join(t.TempDir(), "nope.mfl")})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func withCapturedStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestCmdCheckTestParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.mfl")
	if err := os.WriteFile(path, []byte("this is not valid mfl {{{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := withCapturedStdout(t, func() {
		callErr = cmdCheckTest([]string{"--program", path})
	})
	if callErr != nil {
		t.Fatalf("cmdCheckTest returned error instead of printing: %v", callErr)
	}
	if strings.TrimSpace(out) != "(parse-error)" {
		t.Fatalf("got %q, want (parse-error)", out)
	}
}

func TestCmdCheckTestCheckError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unbound.mfl")
	// parses fine but references an undefined function, so Check() fails.
	if err := os.WriteFile(path, []byte("func main(){x:=undefinedFn(1)}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := withCapturedStdout(t, func() {
		callErr = cmdCheckTest([]string{"--program", path})
	})
	if callErr != nil {
		t.Fatalf("cmdCheckTest returned error instead of printing: %v", callErr)
	}
	if strings.TrimSpace(out) != "(check-error)" {
		t.Fatalf("got %q, want (check-error)", out)
	}
}

func TestCmdCheckTestProgram(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.mfl")
	src := "func add(a,b){return a+b}\n\nfunc main(){x:=add(1,2)}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := withCapturedStdout(t, func() {
		callErr = cmdCheckTest([]string{"--program", path})
	})
	if callErr != nil {
		t.Fatalf("cmdCheckTest: %v", callErr)
	}
	for _, want := range []string{"(func add", "(param a int)", "(param b int)", "(ret 0 int)", "(func main", "(local x int)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q, got:\n%s", want, out)
		}
	}
	// "add" sorts before "main", so its row must come first.
	if strings.Index(out, "(func add") > strings.Index(out, "(func main") {
		t.Fatalf("rows not sorted by key, got:\n%s", out)
	}
}

// cmdCheckTest with empty program (no functions) returns empty output.
func TestCmdCheckTestEmptyProgram(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.mfl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := withCapturedStdout(t, func() {
		callErr = cmdCheckTest([]string{"--program", path})
	})
	if callErr != nil {
		t.Fatalf("cmdCheckTest: %v", callErr)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("empty program should produce no output, got: %q", out)
	}
}
