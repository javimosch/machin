package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `machin cgentest` builds its cgen by hand rather than through the normal
// path, so every memo map has to be initialized there explicitly. A missing one
// is not a wrong answer — it is a nil-map assignment, which panics the moment
// the first program uses that feature.
//
// This is the path the codegen oracle runs over its corpus, so the crash waits
// silently until someone adds a program using the feature to the corpus.

func cgentestOut(t *testing.T, src string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "machin")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Skipf("cannot build the compiler here: %v\n%s", err, out)
	}
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "p.src")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mfl, err := exec.Command(bin, "encode", srcPath).Output()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	mflPath := filepath.Join(dir, "p.mfl")
	if err := os.WriteFile(mflPath, mfl, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "cgentest", "--program", mflPath).CombinedOutput()
	if err != nil {
		t.Fatalf("cgentest: %v\n%s", err, out)
	}
	return string(out)
}

// copy() emits a per-type deep copier through copyMemo.
func TestCGenTestEmitsCopy(t *testing.T) {
	out := cgentestOut(t, `type Box struct { v []int }
func main() { a := Box{}  a.v = []int{1}  b := copy(a)  println(str(len(b.v))) }`)
	if strings.Contains(out, "panic") {
		t.Fatalf("cgentest panicked:\n%s", out)
	}
	if !strings.Contains(out, "mfl_copy_v") {
		t.Fatalf("no deep copier emitted:\n%s", out)
	}
}

// A `go` statement must check pthread_create. Ignoring its result means that
// when the system is out of threads — a loaded CI box, a low RLIMIT_NPROC —
// the goroutine silently never runs. Measured on the old emitter with
// RLIMIT_NPROC set just above the current thread count: ZERO of 200 goroutines
// ran, the program printed "main finished", and it exited 0. A program that
// did none of its work and reported success is the shape every "flaky" test
// eventually turns out to be.
//
// (`t` is also left uninitialized on failure, and detaching it is undefined
// behaviour, which is its own reason not to do this.)
func TestGoStatementChecksPthreadCreate(t *testing.T) {
	out := cgentestOut(t, `func work(n) { println(str(n)) }
func main() { go work(1)  sleep(10) }`)
	if !strings.Contains(out, "pthread_create") {
		t.Fatalf("no spawn emitted:\n%s", out)
	}
	if !strings.Contains(out, "if (pthread_create(") {
		t.Fatalf("pthread_create's result is not checked:\n%s", out)
	}
	if !strings.Contains(out, "go: pthread_create") {
		t.Fatalf("a failed spawn does not name itself:\n%s", out)
	}
}

// sort_by emits a comparator through sortMemo — the same shape, and it was
// already missing before copy was added.
func TestCGenTestEmitsSortBy(t *testing.T) {
	out := cgentestOut(t, `func main() {
    xs := []int{3, 1, 2}
    ys := sort_by(xs, func(a, b) { return a < b })
    println(str(ys[0]))
}`)
	if strings.Contains(out, "panic") {
		t.Fatalf("cgentest panicked:\n%s", out)
	}
	if !strings.Contains(out, "mfl_ltby") {
		t.Fatalf("no comparator emitted:\n%s", out)
	}
}
