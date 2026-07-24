package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// buildRunResetProg builds a full program (types + funcs) and returns its stdout.
func buildRunResetProg(t *testing.T, decls ...string) string {
	t.Helper()
	nd := make([]string, len(decls))
	for i, d := range decls {
		nd[i] = normalize(d)
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-areset-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return string(out)
}

// #523: arena_reset() frees the current goroutine's value-arena chain in place,
// without ending the goroutine — the escape hatch that keeps a long-running
// single-actor server's RSS flat. Semantics must be preserved: resetting between
// iterations must not change the (scalar) result, and freshly allocated strings
// after a reset must be correct.
func TestArenaResetPreservesResult(t *testing.T) {
	churn := `func churn(reset) (acc) {
	acc = 0
	i := 0
	while i < 5000 {
		s := "row-" + str(i) + "-payload-" + str(i * 7)
		acc = acc + len(s)
		if reset == 1 { if i % 100 == 0 { arena_reset() } }
		i = i + 1
	}
}`
	main := `func main() {
	a := churn(0)
	b := churn(1)
	println(str(a) + " " + str(b))
}`
	out := strings.TrimSpace(buildRunResetProg(t, churn, main))
	parts := strings.Fields(out)
	if len(parts) != 2 {
		t.Fatalf("unexpected output %q", out)
	}
	if parts[0] != parts[1] {
		t.Fatalf("arena_reset changed the result: without=%s with=%s", parts[0], parts[1])
	}
}

// A string allocated AFTER a reset must be fully usable (the reset only reclaims
// what was live before it, and the arena keeps allocating from a fresh chain).
func TestArenaResetFreshAllocUsable(t *testing.T) {
	prog := `func main() {
	a := "before-" + str(len("seed"))
	arena_reset()
	b := "after-" + str(len(a))
	println(b)
}`
	out := strings.TrimSpace(buildRunResetProg(t, prog))
	// len("before-4") == 8, so b == "after-8"
	if out != "after-8" {
		t.Fatalf("fresh alloc after reset wrong: got %q want %q", out, "after-8")
	}
}

// arena_reset takes no arguments (checked by the type checker, not the parser).
func TestArenaResetArity(t *testing.T) {
	prog, err := ParseProgram([]string{normalize(`func main() { arena_reset(1) }`)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-areset-arity-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	err = BuildBinary(prog, bin.Name(), false)
	if err == nil {
		t.Fatalf("expected an arity error for arena_reset(1)")
	}
	if !strings.Contains(err.Error(), "arena_reset") {
		t.Fatalf("arity error should mention arena_reset, got: %v", err)
	}
}
