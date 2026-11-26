package main

// #725: exec() captures through temp files in $TMPDIR (default /tmp). The path
// is spliced into a shell command, so it must be quoted.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecHonoursTMPDIR(t *testing.T) {
	prog, err := wallsCompose(t, `func main() {
    code, out, errs := exec("echo to-out; echo to-err >&2; ls \"${TMPDIR:-/tmp}\" | grep -c mfl-exec; exit 3")
    println(code)
    print(out)
    print(errs)
}`)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "p")
	if err := BuildBinary(prog, bin, false); err != nil {
		t.Fatal(err)
	}
	run := func(tmpdir string, set bool) string {
		cmd := exec.Command(bin)
		cmd.Dir = t.TempDir()
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "TMPDIR=") {
				cmd.Env = append(cmd.Env, e)
			}
		}
		if set {
			cmd.Env = append(cmd.Env, "TMPDIR="+tmpdir)
		}
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("TMPDIR=%q: %v", tmpdir, err)
		}
		if left, _ := os.ReadDir(cmd.Dir); len(left) != 0 {
			t.Errorf("TMPDIR=%q: the path ran as shell code, cwd now has %v", tmpdir, left)
		}
		return string(out)
	}
	// both capture files live in TMPDIR while the command runs, and are removed
	for _, name := range []string{"plain", "with space", `it's "q" $(touch pwned) ; touch pwned2 & x`} {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if got, want := run(dir, true), "3\nto-out\n2\nto-err\n"; got != want {
			t.Errorf("%q: got %q want %q", name, got, want)
		}
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Errorf("%q: capture files left behind: %v", name, left)
		}
	}
	// unset and empty both mean /tmp
	for _, set := range []bool{false, true} {
		if got := run("", set); !strings.HasPrefix(got, "3\nto-out\n") {
			t.Errorf("default (set=%v): got %q", set, got)
		}
	}
	// a TMPDIR that does not exist fails the call rather than writing elsewhere
	if got := run("/no/such/dir", true); got != "-1\n" {
		t.Errorf("missing dir: got %q", got)
	}
}
