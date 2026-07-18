package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const buggyProg = `func avg(xs){total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}
func firstgap(xs){return xs[len(xs)-5]}
func safe(xs){if len(xs)==0{return 0}return xs[0]}
func main(){println(str(avg([]int{1,2})),str(firstgap([]int{3})),str(safe([]int{})))}
`

// TestFalsifyInCheck confirms the check.go integration: falsify findings surface
// as advisory Warnings and do NOT flip OK or the error count.
func TestFalsifyInCheck(t *testing.T) {
	res := analyzeSource(buggyProg, []string{"buggy.mfl"})
	if !res.OK || res.ErrorCount != 0 {
		t.Fatalf("advisory findings must not fail check: OK=%v errors=%d", res.OK, res.ErrorCount)
	}
	codes := map[string]bool{}
	for _, w := range res.Warnings {
		if w.Phase != "falsify" || w.Severity != "warning" {
			t.Fatalf("bad warning: %+v", w)
		}
		codes[w.Code] = true
	}
	if !codes["FALS001"] || !codes["FALS002"] {
		t.Fatalf("want FALS001+FALS002 warnings, got %v", codes)
	}
}

// TestFalsifyCmdJSON drives the --json driver and validates the verdict envelope.
func TestFalsifyCmdJSON(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "buggy.mfl")
	if err := os.WriteFile(src, []byte(buggyProg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureFalsifyStdout(t, func() {
		if err := cmdFalsify([]string{"--json", src}); err != nil {
			t.Fatalf("cmdFalsify: %v", err)
		}
	})
	var rep falsifyReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if rep.OK || rep.Counterexamples != 2 {
		t.Fatalf("want ok=false, 2 counterexamples; got ok=%v n=%d", rep.OK, rep.Counterexamples)
	}
	if rep.Coverage.Checked == 0 || rep.Coverage.AllUnknown == 0 {
		t.Fatalf("coverage envelope looks wrong: %+v", rep.Coverage)
	}
	// honesty surface (Slice 1.4): per-function verdicts, bounds, never "proved".
	if rep.Bounds.SliceLenMax != falsSliceLenMax || rep.Bounds.CallDepth != falsCallDepth {
		t.Fatalf("bounds not reported: %+v", rep.Bounds)
	}
	vs := map[string]string{}
	for _, fv := range rep.Functions {
		vs[fv.Fn] = fv.Verdict
		if fv.Verdict == "proved" {
			t.Fatal("the falsifier must NEVER claim proved")
		}
	}
	if vs["avg"] != "counterexample" || vs["safe"] != "clean" || vs["main"] != "unknown" {
		t.Fatalf("per-function verdicts wrong: %v", vs)
	}
	if strings.Contains(out, "proved") {
		t.Fatal("envelope must not contain the word proved")
	}
}

// TestFalsifyCmdStrict confirms --strict exits non-zero only on a counterexample.
func TestFalsifyCmdStrict(t *testing.T) {
	bin := "bin/machin"
	if _, e := os.Stat(bin); e != nil {
		t.Skipf("no bin/machin: %v", e)
	}
	abs, _ := filepath.Abs(bin)
	dir := t.TempDir()
	buggy := filepath.Join(dir, "buggy.mfl")
	clean := filepath.Join(dir, "clean.mfl")
	os.WriteFile(buggy, []byte(buggyProg), 0o644)
	os.WriteFile(clean, []byte("func safe(xs){if len(xs)==0{return 0}return xs[0]}\nfunc main(){println(str(safe([]int{1})))}\n"), 0o644)

	if err := exec.Command(abs, "falsify", "--strict", buggy).Run(); err == nil {
		t.Fatal("--strict must exit non-zero when a counterexample exists")
	}
	if err := exec.Command(abs, "falsify", "--strict", clean).Run(); err != nil {
		t.Fatalf("--strict must exit 0 on a clean program, got %v", err)
	}
}

// TestFalsifyCmdRepro drives --repro and confirms each emitted repro is a real
// bug: it builds with --safe and panics.
func TestFalsifyCmdRepro(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "buggy.mfl")
	if err := os.WriteFile(src, []byte(buggyProg), 0o644); err != nil {
		t.Fatal(err)
	}
	reproDir := filepath.Join(dir, "repros")
	if err := cmdFalsify([]string{"--repro", reproDir, src}); err != nil {
		t.Fatalf("cmdFalsify --repro: %v", err)
	}
	entries, err := os.ReadDir(reproDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 repros, got %d", len(entries))
	}
	bin := "bin/machin"
	if _, e := os.Stat(bin); e != nil {
		t.Skipf("no bin/machin to run repros: %v", e)
	}
	abs, _ := filepath.Abs(bin)
	for _, e := range entries {
		out, err := exec.Command(abs, "run", "--safe", filepath.Join(reproDir, e.Name())).CombinedOutput()
		if err == nil {
			t.Fatalf("repro %s did not panic:\n%s", e.Name(), out)
		}
		low := strings.ToLower(string(out))
		if !strings.Contains(low, "out of range") && !strings.Contains(low, "by zero") {
			t.Fatalf("repro %s wrong trap:\n%s", e.Name(), out)
		}
	}
}

// TestFalsifyCmdErrors covers the non-typechecking and no-input paths.
func TestFalsifyCmdErrors(t *testing.T) {
	if err := cmdFalsify(nil); err == nil {
		t.Fatal("want error for no input")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.mfl")
	os.WriteFile(bad, []byte(`func f(x){return x+}`), 0o644)
	if err := cmdFalsify([]string{bad}); err == nil {
		t.Fatal("want error for non-parsing program")
	}
}

func captureFalsifyStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}
