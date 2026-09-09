package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// #662: eprint/eprintln are print/println on the process stderr stream, so a tool
// can keep JSON on stdout and progress on stderr without write_file("/dev/stderr")
// — which re-opens with O_TRUNC and wipes a redirected log on every call.

func runSplit(t *testing.T, src string) (string, string) {
	t.Helper()
	bin, err := os.CreateTemp("", "mfl-eprint-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(bin.Name())
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	_ = cmd.Run()
	return so.String(), se.String()
}

func TestEprintlnGoesToStderrOnly(t *testing.T) {
	out, errs := runSplit(t, `func main() { eprintln("progress", 1, 2.5, true)  eprint("no-newline")  eprintln()  println("{\"ok\":true}") }`)
	if out != "{\"ok\":true}\n" {
		t.Fatalf("stdout must carry only println output, got %q", out)
	}
	if errs != "progress 1 2.5 true\nno-newline\n" {
		t.Fatalf("stderr formatting must match println's, got %q", errs)
	}
}

func TestEprintlnAppendsToRedirectedStderr(t *testing.T) {
	// the whole point: a `2> log` redirect keeps every line, not just the last one.
	bin, err := os.CreateTemp("", "mfl-eprint-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, `func main() { i := 0  while i < 5 { eprintln("line", i)  i = i + 1 } }`)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	log, err := os.CreateTemp("", "mfl-eprint-log-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(log.Name())
	cmd := exec.Command(bin.Name())
	cmd.Stderr = log
	_ = cmd.Run()
	log.Close()
	data, _ := os.ReadFile(log.Name())
	if got := strings.Count(string(data), "\n"); got != 5 {
		t.Fatalf("redirected stderr must keep all 5 lines, got %d: %q", got, string(data))
	}
}

func TestEprintlnIsStatementOnly(t *testing.T) {
	_, err := ParseProgram([]string{normalize(`func main() { x := eprintln("a")  println(str(x)) }`)})
	if err == nil {
		prog, _ := ParseProgram([]string{normalize(`func main() { x := eprintln("a")  println(str(x)) }`)})
		bin, _ := os.CreateTemp("", "mfl-eprint-*")
		bin.Close()
		defer os.Remove(bin.Name())
		if err := BuildBinary(prog, bin.Name(), false); err == nil {
			t.Fatalf("eprintln used as an expression must be rejected")
		}
	}
}
