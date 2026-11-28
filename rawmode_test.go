package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// A program that dies in raw mode must leave the terminal as it found it (#756).
// Python drives the pseudo-terminal and compares termios before and after.
const rawModeDriver = `
import os, pty, signal, sys, termios, time
def run(how):
    m, s = pty.openpty()
    before = termios.tcgetattr(s)
    pid = os.fork()
    if pid == 0:
        os.setsid()
        t = os.open(os.ttyname(s), os.O_RDWR)
        for fd in (0, 1, 2): os.dup2(t, fd)
        os.execv(sys.argv[1], [sys.argv[1], how])
    out = b""
    while b"ready" not in out: out += os.read(m, 100)
    during = termios.tcgetattr(s)
    if how in ("int", "term", "hup", "quit"):
        os.kill(pid, getattr(signal, "SIG" + how.upper()))
    if how == "byte":
        os.write(m, b"\x03")
    _, st = os.waitpid(pid, 0)
    sig = os.WTERMSIG(st) if os.WIFSIGNALED(st) else 0
    print(how, sig, bool(during[3] & termios.ISIG), termios.tcgetattr(s) == before)
for how in ("int", "term", "hup", "quit", "exit", "byte"): run(how)
`

func TestRawModeRestoresTheTerminal(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil || runtime.GOOS != "linux" {
		t.Skip("needs linux and python3")
	}
	prog := progFromSrc(t, `func main() {
    av := args()
    how := av[1]
    raw_mode(1)
    if how == "byte" { raw_mode(2) }
    println("ready")
    flush()
    if how == "exit" { exit(0) }
    if how == "byte" {
        for read_key() == "" { sleep(5) }
        exit(0)
    }
    sleep(5000)
}`)
	bin := filepath.Join(t.TempDir(), "raw")
	if err := BuildBinary(prog, bin, false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command("timeout", "20", py, "-c", rawModeDriver, bin).CombinedOutput()
	want := "int 2 True True\nterm 15 True True\nhup 1 True True\nquit 3 True True\nexit 0 True True\nbyte 0 False True\n"
	if err != nil || string(out) != want {
		t.Fatalf("%v\ngot:\n%s\nwant:\n%s", err, out, want)
	}
}
