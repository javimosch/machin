package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// #660: a root (goroutine/main) arena that grows past MFL_ARENA_WARN_MB with no
// arena { } block active warns once on stderr; a per-iteration arena block never
// trips it; MFL_ARENA_WARN_MB=0 disables it. The program's stdout is unaffected.

func tripwireRun(t *testing.T, src, warnMB string) (string, string) {
	t.Helper()
	bin, err := os.CreateTemp("", "mfl-tripwire-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(bin.Name())
	cmd.Env = append(os.Environ(), "MFL_ARENA_WARN_MB="+warnMB)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	_ = cmd.Run()
	return so.String(), se.String()
}

const tripwireLoop = `func main() { total := 0  i := 0  while i < 200000 { s := "row-" + str(i) + "-padding-padding-padding"  total = total + len(s)  i = i + 1 }  println(str(total)) }`
const tripwireScoped = `func main() { total := 0  i := 0  while i < 200000 { arena { s := "row-" + str(i) + "-padding-padding-padding"  total = total + len(s) }  i = i + 1 }  println(str(total)) }`

func TestArenaTripwireWarnsOnRootArenaGrowth(t *testing.T) {
	out, errs := tripwireRun(t, tripwireLoop, "2")
	if strings.TrimSpace(out) != "6688890" {
		t.Fatalf("stdout changed: %q", out)
	}
	if strings.Count(errs, "goroutine arena passed 2 MB") != 1 {
		t.Fatalf("want exactly one tripwire warning on stderr, got: %q", errs)
	}
}

func TestArenaTripwireSilentInsideArenaBlock(t *testing.T) {
	out, errs := tripwireRun(t, tripwireScoped, "2")
	if strings.TrimSpace(out) != "6688890" || strings.Contains(errs, "arena passed") {
		t.Fatalf("per-iteration arena must not trip: stdout=%q stderr=%q", out, errs)
	}
}

func TestArenaTripwireDisabled(t *testing.T) {
	_, errs := tripwireRun(t, tripwireLoop, "0")
	if strings.Contains(errs, "arena passed") {
		t.Fatalf("MFL_ARENA_WARN_MB=0 must silence the tripwire: %q", errs)
	}
}
