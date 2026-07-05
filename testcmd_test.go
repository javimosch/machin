package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runMFLTests is the pure core of `machin test` — no I/O, no exit — so we
// assert on the returned TestRunResult directly, the same pattern
// analyzeSource (machin check's core) uses.

func writeSrc(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunMFLTestsAllPass(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() {
    assert(1 + 1 == 2, "addition")
    assert_eq_int(3, 3, "int eq")
    assert_eq_str("a", "a", "str eq")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK || res.Passed != 3 || res.Failed != 0 {
		t.Fatalf("expected 3 passed/0 failed/ok, got %+v", res)
	}
}

func TestRunMFLTestsSomeFail(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() {
    assert(true, "pass 1")
    assert(1 == 2, "deliberate failure")
    assert_eq_int(1, 2, "another deliberate failure")
    test_summary()
}`)
	res, out, err := runMFLTests([]string{f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK || res.Passed != 1 || res.Failed != 2 {
		t.Fatalf("expected 1 passed/2 failed/not-ok, got %+v", res)
	}
	if !strings.Contains(out, "FAIL: deliberate failure") {
		t.Errorf("program output should include the FAIL line, got %q", out)
	}
}

func TestRunMFLTestsComposeError(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() { undefined_thing_xyz() }`)
	_, _, err := runMFLTests([]string{f})
	if err == nil {
		t.Fatal("expected a compose/typecheck error, got nil")
	}
}

func TestRunMFLTestsMissingSummary(t *testing.T) {
	dir := t.TempDir()
	// A test that asserts but never calls test_summary() — no TEST_SUMMARY
	// line, so runMFLTests must report an error, not silently claim 0/0 ok.
	f := writeSrc(t, dir, "t.src", `func main() { assert(true, "ok") }`)
	_, _, err := runMFLTests([]string{f})
	if err == nil {
		t.Fatal("expected an error for a test that never calls test_summary()")
	}
}

func TestRunMFLTestsComposesFrameworkModule(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() {
    fs := new_flags("x")
    fs = flag_int(fs, "n", "n", "1", "count")
    assert_eq_int(flag_int_val(fs, "n"), 1, "default")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{"framework/flags.src", f})
	if err != nil {
		t.Fatalf("unexpected error composing flags.src ahead of the test: %v", err)
	}
	if !res.OK || res.Passed != 1 {
		t.Fatalf("expected 1 passed/ok, got %+v", res)
	}
}

func TestRunMFLTestsNoFiles(t *testing.T) {
	if _, _, err := runMFLTests(nil); err == nil {
		t.Fatal("expected an error with no files given")
	}
}

func TestCmdTestNoFiles(t *testing.T) {
	err := cmdTest(nil)
	if err == nil {
		t.Fatal("expected error with no files")
	}
}

func TestCmdTestWithJSONOutput(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() {
	    assert(1 + 1 == 2, "addition")
	    test_summary()
	}`)
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = cmdTest([]string{"--json", f})
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "\"ok\": true") && !strings.Contains(output, "\"ok\":true") {
		t.Fatalf("JSON output should contain ok:true, got %q", output)
	}
}

func TestCmdTestWithJSONOutputAndFailures(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() {
	    assert(true, "pass")
	    assert(1 == 2, "failure")
	    test_summary()
	}`)
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = cmdTest([]string{"--json", f})
	w.Close()
	os.Stdout = old
	if err == nil {
		t.Fatal("expected an error when tests fail")
	}
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "\"ok\": false") && !strings.Contains(output, "\"ok\":false") {
		t.Fatalf("JSON output should contain ok:false for failing tests, got %q", output)
	}
	if !strings.Contains(output, "\"failed\": 1") && !strings.Contains(output, "\"failed\":1") {
		t.Fatalf("JSON output should contain failed:1, got %q", output)
	}
}

func TestParseTestSummary(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		wantPass int
		wantFail int
		wantOK   bool
	}{
		{"clean", "TEST_SUMMARY passed=5 failed=0", 5, 0, true},
		{"with failures and FAIL lines", "FAIL: x\nFAIL: y\nTEST_SUMMARY passed=1 failed=2", 1, 2, true},
		{"no summary line", "just some program output\n", 0, 0, false},
		{"takes the LAST summary line", "TEST_SUMMARY passed=1 failed=0\nTEST_SUMMARY passed=9 failed=9", 9, 9, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, f, ok := parseTestSummary(c.out)
			if ok != c.wantOK || p != c.wantPass || f != c.wantFail {
				t.Errorf("parseTestSummary(%q) = (%d, %d, %v), want (%d, %d, %v)", c.out, p, f, ok, c.wantPass, c.wantFail, c.wantOK)
			}
		})
	}
}
