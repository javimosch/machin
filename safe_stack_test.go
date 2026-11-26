package main

// #719 (--safe stack-overflow guard) and #718 (non-scalar slice literals).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runSafe(t *testing.T, src string, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	prog, err := wallsCompose(t, src)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "p")
	if err := BuildBinary(prog, bin, true); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), env...)
	var o, e strings.Builder
	cmd.Stdout, cmd.Stderr = &o, &e
	_ = cmd.Run()
	return o.String(), e.String(), cmd.ProcessState.ExitCode()
}

func TestSafeStackOverflowIsReported(t *testing.T) {
	for name, src := range map[string]string{
		"recursion": `func down(n) (r) { r = down(n + 1) + 1 }
func main() { println("before")  println(down(0)) }`,
		// cc -O2 can turn this into a loop that uses no stack: the count must catch it.
		"tail": `func f(n) (r) { r = f(n + 1) }
func main() { println("before")  println(f(0)) }`,
		"goroutine": `func down(n) (r) { r = down(n + 1) + 1 }
func work(c) { c <- down(0) }
func main() { println("before")  c := make(chan int)  go work(c)  println(<-c) }`,
		"closure": `func main() { println("before")  fs := make([]func, 1)  fs[0] = func(n) { g := fs[0]  return g(n + 1) + 1 }  g := fs[0]  println(g(0)) }`,
	} {
		out, errs, code := runSafe(t, src)
		if code != 1 || out != "before\n" || !strings.Contains(errs, "panic: stack overflow") {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", name, code, out, errs)
		}
	}
}

func TestSafeDepthCountsReturnsAndHonoursOverride(t *testing.T) {
	// Many more calls than the limit, never deeper than it: every return path
	// must give its level back.
	src := `func down(n) (r) { if n == 0 { return 0 }  r = down(n - 1) + 1 }
func early(n) { if n > 0 { return 1 }  return 0 }
func main() { s := 0  i := 0  for i < 500 { s = s + down(50) + early(i)  i = i + 1 }  println(s) }`
	if out, errs, code := runSafe(t, src, "MFL_MAX_DEPTH=100"); code != 0 || out != "25499\n" {
		t.Errorf("within limit: code=%d stdout=%q stderr=%q", code, out, errs)
	}
	if _, errs, code := runSafe(t, src, "MFL_MAX_DEPTH=20"); code != 1 || !strings.Contains(errs, "depth limit") {
		t.Errorf("over limit: code=%d stderr=%q", code, errs)
	}
}

func TestNestedSliceLiterals(t *testing.T) {
	prog, err := wallsCompose(t, `
func feed(c) { c <- 9 }
func main() {
    a := [][]string{[]string{"x"}, []string{"y", "z"}}
    println(a[1][0], a[1][1], len(a))
    b := [][][]int{[][]int{[]int{1}}}
    println(b[0][0][0], len([][]int{}))
    m := []map[string]int{}
    mm := make(map[string]int)
    mm["k"] = 3
    ms := []map[string]int{mm}
    println(len(m), ms[0]["k"])
    ch := []chan int{make(chan int)}
    go feed(ch[0])
    println(<-ch[0])
    a = append(a, []string{"w"})
    println(a[2][0], a[0][0])
}`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(strings.Fields(out), " "), "y z 2 1 0 0 3 9 w x"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
