package main

import (
	"os"
	"os/exec"
	"testing"
)

// TestCLIBuiltins exercises args() (command-line arguments), env() (environment),
// and now() (wall-clock seconds) — the basis for building a CLI in MFL.
func TestCLIBuiltins(t *testing.T) {
	src := `func main(){ a := args() println(len(a)) println(a[1]) println(env("MFL_TEST_VAR")) println(now() > 0) }`
	prog := &Program{Funcs: parseFuncs(t, src)}
	bin, err := os.CreateTemp("", "mfl-cli-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(bin.Name(), "serve", "--port")
	cmd.Env = append(os.Environ(), "MFL_TEST_VAR=hello")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// argv = [binary, serve, --port] -> len 3, a[1]="serve"; env set; now>0
	if got, want := string(out), "3\nserve\nhello\ntrue\n"; got != want {
		t.Fatalf("CLI builtins: got %q, want %q", got, want)
	}
}
