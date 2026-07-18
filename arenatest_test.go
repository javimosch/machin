package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArenaTestCommand exercises the `machin arenatest` oracle command (the self-hosting
// oracle for arena escape analysis): it dumps ARENA00x findings in the canonical hex form,
// and handles the no-arg / missing-file / parse-error paths. (captureStdoutDL is shared.)
func TestArenaTestCommand(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// a program whose arena block leaks a value to a named return — expect an ARENA001 line.
	esc := write("esc.mfl", "func f() (r) { arena { s := \"x\" + str(1)  r = s } }\nfunc main() { println(f()) }\n")
	out := captureStdoutDL(t, func() {
		if err := cmdArenaTest([]string{"--program", esc}); err != nil {
			t.Fatalf("arenatest: %v", err)
		}
	})
	if !strings.Contains(out, "|4152454e41303031|") { // hex("ARENA001")
		t.Fatalf("expected an ARENA001 hex line, got %q", out)
	}

	// a return inside an arena block — expect ARENA002.
	ret := write("ret.mfl", "func f(n) (r) { arena { return \"v\" + str(n) }  r = \"\" }\nfunc main() { println(f(1)) }\n")
	if out := captureStdoutDL(t, func() { _ = cmdArenaTest([]string{"--program", ret}) }); !strings.Contains(out, "|4152454e41303032|") { // hex("ARENA002")
		t.Fatalf("expected an ARENA002 hex line, got %q", out)
	}

	// a clean arena program — no findings, empty output.
	clean := write("clean.mfl", "func f(n) (r) { total := 0  arena { s := \"i\" + str(n)  total = total + len(s) }  r = total }\nfunc main() { println(str(f(3))) }\n")
	if out := captureStdoutDL(t, func() { _ = cmdArenaTest([]string{"--program", clean}) }); strings.TrimSpace(out) != "" {
		t.Fatalf("clean program should dump nothing, got %q", out)
	}

	// error / edge paths.
	if err := cmdArenaTest(nil); err == nil {
		t.Fatal("expected usage error with no --program")
	}
	if err := cmdArenaTest([]string{"--program", filepath.Join(dir, "nope.mfl")}); err == nil {
		t.Fatal("expected error for a missing file")
	}
	perr := write("perr.mfl", "func main() { x := }\n")
	if out := captureStdoutDL(t, func() { _ = cmdArenaTest([]string{"--program", perr}) }); !strings.Contains(out, "(parse-error)") && !strings.Contains(out, "(check-error)") {
		t.Fatalf("malformed source should report parse/check-error, got %q", out)
	}
}
