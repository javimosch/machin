package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// framework/flags.src composed with an app must parse short/long flags, the
// `=` and space value forms, bool flags, defaults, and positionals — exercised
// by building the real binary and passing it argv (parse_flags reads args()).
func TestFlagsModule(t *testing.T) {
	mod, err := os.ReadFile("framework/flags.src")
	if err != nil {
		t.Fatal(err)
	}
	app := `func yn(b) (s) { s = "false"  if b { s = "true" } }
func main() {
	fs := new_flags("t")
	fs = flag_str(fs, "out", "o", "-", "output")
	fs = flag_int(fs, "count", "c", "1", "count")
	fs = flag_bool(fs, "verbose", "v", "verbose")
	fs = parse_flags(fs, args())
	r := flag_args(fs)
	pos := ""
	for _, x := range r { pos = pos + x }
	println(flag_get(fs, "out") + "|" + str(flag_int_val(fs, "count")) + "|" + yn(flag_on(fs, "verbose")) + "|" + pos)
}`
	blocks, err := splitFunctions(string(mod) + "\n" + app)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var decls []string
	for _, b := range blocks {
		decls = append(decls, normalize(b))
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-flags-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	// --count=5 (long+inline), -o file (short+space), -v (bool), two positionals
	out, err := exec.Command(bin.Name(), "--count=5", "-o", "file", "-v", "a", "b").Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(out)) != "file|5|true|ab" {
		t.Fatalf("flags parse: got %q, want %q", strings.TrimSpace(string(out)), "file|5|true|ab")
	}
}
