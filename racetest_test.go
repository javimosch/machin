package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdRaceTest is the self-hosting race oracle; cover its error paths and happy cases:
// missing args, missing --program flag, file not found, parse error, and successful detection.
func TestCmdRaceTestMissingArgs(t *testing.T) {
	if err := cmdRaceTest(nil); err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestCmdRaceTestMissingProgramFlag(t *testing.T) {
	if err := cmdRaceTest([]string{"--bogus", "file.mfl"}); err == nil {
		t.Fatal("expected error for missing --program flag")
	}
}

func TestCmdRaceTestFileNotFound(t *testing.T) {
	if err := cmdRaceTest([]string{"--program", "/nonexistent/path/file.mfl"}); err == nil {
		t.Fatal("expected error for file not found")
	}
}

// withCapturedStdout is already defined in checktest_test.go (same package);
// reuse it here instead of redeclaring it.

func TestCmdRaceTestParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.mfl")
	if err := os.WriteFile(path, []byte("this is not valid mfl {{{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := withCapturedStdout(t, func() {
		callErr = cmdRaceTest([]string{"--program", path})
	})
	if callErr != nil {
		t.Fatalf("cmdRaceTest returned error instead of printing: %v", callErr)
	}
	if strings.TrimSpace(out) != "(parse-error)" {
		t.Fatalf("got %q, want (parse-error)", out)
	}
}

func TestCmdRaceTestCleanProgram(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.mfl")
	// A program with no races: single main function
	src := "func main(){x:=1}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := withCapturedStdout(t, func() {
		callErr = cmdRaceTest([]string{"--program", path})
	})
	if callErr != nil {
		t.Fatalf("cmdRaceTest: %v", callErr)
	}
	// Clean program should have no output (no findings)
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected empty output for clean program, got: %q", out)
	}
}

func TestCmdRaceTestRaceFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race.mfl")
	// Two goroutines writing to the same global variable
	src := "func worker(id){data:=100 println(id)} func main(){data:=0 go worker(1) go worker(2) println(data)}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := withCapturedStdout(t, func() {
		callErr = cmdRaceTest([]string{"--program", path})
	})
	if callErr != nil {
		t.Fatalf("cmdRaceTest: %v", callErr)
	}
	// Should detect races (finding count depends on the race analysis)
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected race findings, got empty output")
	}
}
