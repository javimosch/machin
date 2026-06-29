package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// exec(cmd) -> (exit_code, stdout, stderr): captures both streams separately and the
// exit status. The dogfood gap from issue #277 (port mongo-vault to MFL).
func TestExecBuiltin(t *testing.T) {
	bin, err := os.CreateTemp("", "mfl-exec-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	src := `func main() {
		code, out, err := exec("echo OUT; echo ERR 1>&2; exit 7")
		println("c=" + str(code) + " o=" + trim(out) + " e=" + trim(err))
	}`
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, src)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "c=7 o=OUT e=ERR" {
		t.Fatalf("exec capture wrong: got %q, want %q", got, "c=7 o=OUT e=ERR")
	}
}
