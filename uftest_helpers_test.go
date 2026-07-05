package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoolToInt(t *testing.T) {
	if got := boolToInt(true); got != 1 {
		t.Fatalf("boolToInt(true) = %d, want 1", got)
	}
	if got := boolToInt(false); got != 0 {
		t.Fatalf("boolToInt(false) = %d, want 0", got)
	}
}

func TestDumpUF(t *testing.T) {
	c := &Checker{}
	newSlot(c, KVar)              // 0
	s1 := newSlot(c, KSlice)      // 1
	c.elem[s1] = newSlot(c, KInt) // 2
	s2 := newSlot(c, KStruct)     // 3
	c.sname[s2] = "Foo"
	s3 := newSlot(c, KMap) // 4
	c.mkey[s3] = newSlot(c, KString)
	c.mval[s3] = newSlot(c, KBool)
	newFuncSlot(c, &funcSig{params: []int{0}, ret: 2})

	out := dumpUF(c, false)
	for _, want := range []string{"kind=slice", "sname=Foo", "mkey=", "fsig=", "err=0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dumpUF output missing %q, got:\n%s", want, out)
		}
	}

	failedOut := dumpUF(c, true)
	if !strings.Contains(failedOut, "err=1") {
		t.Fatalf("dumpUF(failed) missing err=1, got:\n%s", failedOut)
	}
}

func TestCmdUFTestScript(t *testing.T) {
	script := `# comment line
var
num
int
float
bool
string
void
bytes
slice 2
chan 2
struct Bar
map 0 1
func 0,1|2
union 2 3
dump
`
	dir := t.TempDir()
	path := filepath.Join(dir, "script.uf")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdUFTest([]string{path}); err != nil {
		t.Fatalf("cmdUFTest() error = %v", err)
	}
}

func TestCmdUFTestUnionMismatch(t *testing.T) {
	script := "int\nstring\nunion 0 1\ndump\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "script.uf")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdUFTest([]string{path}); err != nil {
		t.Fatalf("cmdUFTest() error = %v", err)
	}
}

func TestCmdUFTestUnknownOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.uf")
	if err := os.WriteFile(path, []byte("bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdUFTest([]string{path}); err == nil {
		t.Fatal("expected error for unknown op, got nil")
	}
}

func TestCmdUFTestMissingFile(t *testing.T) {
	if err := cmdUFTest([]string{filepath.Join(t.TempDir(), "does-not-exist.uf")}); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestCmdUFTestStdin(t *testing.T) {
	script := "int\nfloat\ndump\n"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	w.Close()

	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	if err := cmdUFTest([]string{}); err != nil {
		t.Fatalf("cmdUFTest(stdin) error = %v", err)
	}
}
