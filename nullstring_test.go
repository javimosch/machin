package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// buildRunProg builds a full program (types + funcs) and returns its stdout.
func buildRunProg(t *testing.T, decls ...string) string {
	t.Helper()
	nd := make([]string, len(decls))
	for i, d := range decls {
		nd[i] = normalize(d)
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-null-*")
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

// parse() zero-initializes structs with {0}, so a missing string field is NULL
// (char*), not "". len()/charat took strlen(s) directly, and strlen(NULL) is UB
// (segfault). len() on a string must route through mfl_s() so len(NULL) == 0.
func TestLenOnNullStringFromParse(t *testing.T) {
	out := buildRunProg(t,
		`type U struct { name string  age int }`,
		`func main() { u := parse("{\"age\":5}", U{})  println(str(len(u.name)))  println(u.name + "!") }`,
	)
	// name absent -> NULL, normalized to "": len 0, and concatenation yields "!"
	if strings.TrimSpace(out) != "0\n!" {
		t.Fatalf("len/concat on a NULL parsed string = %q, want \"0\\n!\"", out)
	}
}
