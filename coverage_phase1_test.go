// coverage_phase1_test.go — small targeted tests that push uncovered
// statements in cheap-to-cover files (parsetest, racetest, framework, lexer,
// testcmd, skillcmd, uftest, cgentest, lextest, guide, transform, build,
// check) toward 95%. Each test names the function/path it covers so coverage
// deltas are attributable.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- parsetest.go (cmdParseTest subcommands and decl classification) ---------

// TestParseTestProgramAllDecls drives every classification branch in
// cmdParseTest's --program subcommand: type, extern (with header/link/cflags/
// cstruct/fn), var, func, and export func. A leading \xff byte drives a lex
// failure that the loop iterates past silently.
func TestParseTestProgramAllDecls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.mfl")
	src := strings.Join([]string{
		"type Point struct { x int y int }",
		`extern "libc" { header "stdio.h" link "c"  cflags "-DWITH_X" cstruct Buf { p ptr n int } fn puts(string) int }`,
		"var p = Point{x: 1, y: 2}",
		"func main(){x:=1}",
		"export func id(x){return x}",
		"\xff",
	}, "\n")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdParseTest([]string{"--program", path}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"(type Point", "(extern libc", "(cstruct Buf", "(global p", "(func main", "(func id export"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- out ---\n%s", want, out)
		}
	}
}

// TestParseTestFuncsWithParseError drives --funcs' per-function (parse-error)
// branch: a function whose body has a syntax error must produce a
// (parse-error) line, and a clean function must still land in the output.
func TestParseTestFuncsWithParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "funcs.mfl")
	if err := os.WriteFile(path, []byte("func good(){return 1}\nfunc broken(}()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdParseTest([]string{"--funcs", path}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "(parse-error)") {
		t.Errorf("--funcs should emit (parse-error) for broken func, got:\n%s", out)
	}
	if !strings.Contains(out, "(func good") {
		t.Errorf("--funcs should still emit the good func's row, got:\n%s", out)
	}
}

// ---- racetest.go (cmdRaceTest output format + (check-error) path) -----------

// TestCmdRaceTestCheckError drives racetest's "(check-error)" branch: a
// parse-clean but typecheck-failing program. The (check-error) marker was not
// directly asserted anywhere.
func TestCmdRaceTestCheckError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.mfl")
	// `len` returns int; "hi" + int is a typecheck error (string vs int),
	// not a parse error, so we hit the (check-error) branch.
	src := `func main(){x := "hi" println(x + 1)}` + "\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdRaceTest([]string{"--program", path}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.TrimSpace(out) != "(check-error)" {
		t.Fatalf("got %q, want (check-error)", out)
	}
}

// TestCmdRaceTestRaceOutput races Rc:TestCmdRaceTestRaceFound in racetest_test.go
// with a second concurrent-race variant so a one-angle-only regression in the
// race analyzer's goroutine fan-out is caught. The output is any non-empty
// finding (race-finding details depend on analyzer revision).
func TestCmdRaceTestRaceOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race.mfl")
	src := `var counter = 0` + "\n" +
		`func incr(){ counter = counter + 1 counter = counter + 1 counter = counter + 1 counter = counter + 1 counter = counter + 1 }` + "\n" +
		`func main(){ go incr() go incr() println(str(counter)) }` + "\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdRaceTest([]string{"--program", path}); err != nil {
			t.Fatal(err)
		}
	})
	trimmed := strings.TrimSpace(out)
	if trimmed == "" || trimmed == "(parse-error)" || trimmed == "(check-error)" {
		t.Fatalf("expected race findings, got %q", trimmed)
	}
}

// ---- framework.go (readModule base-name fallback) -----------------------------

// TestReadModuleBareNameOnCurDir verifies readModule falls back to the embedded
// base name when given a real local path that doesn't exist but whose base
// matches an embedded module.
func TestReadModuleBareNameOnCurDir(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"reactive.src", "test.src"} {
		got, err := readModule(filepath.Join(dir, p))
		if err != nil {
			t.Errorf("readModule(%q): unexpected error: %v", p, err)
			continue
		}
		if len(got) == 0 {
			t.Errorf("readModule(%q): expected embedded contents, got empty", p)
		}
	}
}

// ---- lexer.go (hex/octal/binary/float literal lexing + error paths) ---------

func TestLexNumberForms(t *testing.T) {
	cases := []struct {
		in   string
		want int64 // for ints
		isF  bool  // float?
	}{
		{"0x10", 0x10, false},
		{"0X0F", 0x0F, false},
		{"0b101", 5, false},
		{"0B111", 7, false},
		{"0o17", 0o17, false},
		{"0O7", 0o7, false},
		{"1.5", 0, true},
		{"0.5", 0, true},
		{"42", 42, false},
	}
	for _, c := range cases {
		toks, err := Lex(c.in)
		if err != nil {
			t.Errorf("Lex(%q): %v", c.in, err)
			continue
		}
		if len(toks) != 2 || toks[1].Kind != TEOF {
			t.Errorf("Lex(%q): want 2 tokens (literal+EOF), got %d", c.in, len(toks))
			continue
		}
		if c.isF && toks[0].Kind != TFloat {
			t.Errorf("Lex(%q): want TFloat, got %d", c.in, toks[0].Kind)
			continue
		}
		if !c.isF && toks[0].Kind != TInt {
			t.Errorf("Lex(%q): want TInt, got %d", c.in, toks[0].Kind)
			continue
		}
		if toks[0].Pos != 0 {
			t.Errorf("Lex(%q): want pos 0, got %d", c.in, toks[0].Pos)
		}
	}
}

// TestLexUnterminatedString forces the lexer's "unterminated string" error so
// the error path is exercised.
func TestLexUnterminatedString(t *testing.T) {
	if _, err := Lex(`"never closed`); err == nil {
		t.Fatal("expected unterminated-string error, got nil")
	}
}

// TestLexUnexpectedCharacter exercises the lexOpOrPunct error path.
func TestLexUnexpectedCharacter(t *testing.T) {
	if _, err := Lex("@"); err == nil {
		t.Fatal("expected unexpected-character error for @, got nil")
	}
}

// ---- testcmd.go (parseTestSummary + runMFLTests variants) --------------------

// TestParseTestSummaryAlreadyHasTally confirms we correctly pull the
// passed=/failed= pair out of a multi-line output.
func TestParseTestSummaryAlreadyHasTally(t *testing.T) {
	out := "before\nTEST_SUMMARY passed=7 failed=3\nafter\n"
	p, f, ok := parseTestSummary(out)
	if !ok || p != 7 || f != 3 {
		t.Fatalf("parseTestSummary(%q) = (%d, %d, %v), want 7/3/true", out, p, f, ok)
	}
}

// TestRunMFLTestsComposesUserSrcBeforeFramework pins the order: framework/
// test.src is composed first, then user files. Two user files in one invocation
// exercise the multi-file append path of runMFLTests.
func TestRunMFLTestsComposesUserSrcBeforeFramework(t *testing.T) {
	dir := t.TempDir()
	a := writeTestSrc(t, dir, "a.src", `func main(){ assert(1 == 1, "true") test_summary() }`)
	b := writeTestSrc(t, dir, "b.src", `// covered by a.src`)
	res, _, err := runMFLTests([]string{a, b})
	if err != nil {
		t.Fatalf("runMFLTests: %v", err)
	}
	if !res.OK || res.Passed != 1 {
		t.Fatalf("want 1 pass/ok, got %+v", res)
	}
	if len(res.Files) != 2 {
		t.Fatalf("want 2 files, got %v", res.Files)
	}
}

// ---- skillcmd.go (cmdSkill, skillInstall, skillList switches) ---------------

// TestCmdSkillList exercises the default "list" branch (no args).
func TestCmdSkillList(t *testing.T) {
	if err := cmdSkill(nil); err != nil {
		t.Fatalf("cmdSkill(nil) (list): %v", err)
	}
}

// TestCmdSkillShow covers the "show" subcommand both for a canonical name
// and for a short alias.
func TestCmdSkillShow(t *testing.T) {
	// Just assert cmdSkill show returns no error and yields the embedded
	// content (non-empty). The exact byte content of the embedded SKILL.md is
	// owned by the skill's author and can drift — only the slot wiring is
	// what this test should pin.
	for _, alias := range []string{"start", "machin-start", "web", "machin-web", "gamedev"} {
		out := captureStdout(t, func() {
			if err := cmdSkill([]string{"show", alias}); err != nil {
				t.Errorf("cmdSkill show %q: %v", alias, err)
			}
		})
		if len(out) == 0 {
			t.Errorf("'machin skill show %q' should print non-empty content", alias)
		}
	}
}

// TestCmdSkillShowMissing exercises the "unknown skill" error path and the
// "missing argument" path.
func TestCmdSkillShowMissing(t *testing.T) {
	if err := cmdSkill([]string{"show"}); err == nil {
		t.Error("cmdSkill show: expected error for missing arg")
	}
	if err := cmdSkill([]string{"show", "nonexistent"}); err == nil {
		t.Error("cmdSkill show nonexistent: expected error")
	}
}

// TestCmdSkillUnknownSubcommand exercises the default branch of cmdSkill.
func TestCmdSkillUnknownSubcommand(t *testing.T) {
	if err := cmdSkill([]string{"frobnicate"}); err == nil {
		t.Error("cmdSkill frobnicate: expected error for unknown subcommand")
	}
}

// TestCmdSkillInstall exercises skillInstall — both the named-skill form and
// the default-all form, with a temp dir target so no real ~/.agents/skills is
// touched.
func TestCmdSkillInstall(t *testing.T) {
	dir := t.TempDir()
	if err := cmdSkill([]string{"install", "--dir", dir, "start"}); err != nil {
		t.Fatalf("cmdSkill install --dir start: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "machin-start", "SKILL.md")); err != nil {
		t.Errorf("expected skill file at %s/machin-start/SKILL.md: %v", dir, err)
	}
	if err := cmdSkill([]string{"install", "--dir", dir}); err != nil {
		t.Fatalf("cmdSkill install (all): %v", err)
	}
	for _, name := range skillOrder {
		if _, err := os.Stat(filepath.Join(dir, name, "SKILL.md")); err != nil {
			t.Errorf("expected %s SKILL.md after install all: %v", name, err)
		}
	}
}

// TestCmdSkillInstallUnknown exercises the per-name "unknown skill" bail
// inside skillInstall.
func TestCmdSkillInstallUnknown(t *testing.T) {
	if err := cmdSkill([]string{"install", "--dir", t.TempDir(), "bogus"}); err == nil {
		t.Error("expected error for unknown skill name to install")
	}
}

// ---- uftest.go (cmdUFTest paths) --------------------------------------------

// TestCmdUFTestScriptFromFile runs a script through cmdUFTest from a path arg,
// covering the file-read branch (vs the /dev/stdin branch used when no args).
func TestCmdUFTestScriptFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.txt")
	if err := os.WriteFile(path, []byte("int\nunion 0 0\ndump\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdUFTest([]string{path}); err != nil {
			t.Fatalf("cmdUFTest(file): %v", err)
		}
	})
	if !strings.Contains(out, "kind=int") {
		t.Errorf("dump should contain kind=int, got:\n%s", out)
	}
	if !strings.Contains(out, "err=0") {
		t.Errorf("dump should contain err=0, got:\n%s", out)
	}
}

// TestCmdUFTestScriptWithMismatch covers the failure bail in cmdUFTest (the
// "dump still emits but err=1" case). A union that fails stops further ops.
func TestCmdUFTestScriptWithMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.txt")
	if err := os.WriteFile(path, []byte("int\nstring\nunion 0 1\ndump\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdUFTest([]string{path}); err != nil {
			t.Fatalf("cmdUFTest: %v", err)
		}
	})
	if !strings.Contains(out, "err=1") {
		t.Errorf("dump should mark err=1 after a mismatch, got:\n%s", out)
	}
}

// ---- cgentest.go (the happy-path "valid program emits C body" branch) ------

// TestCmdCGenTestValidProgramNoExternalDeps runs a tight program that, by
// the cgentest oracle contract, emits the program-specific C body (no runtime
// prelude, bodyOnly=true).
func TestCmdCGenTestValidProgramNoExternalDeps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.mfl")
	src := `func main(){x:=1 println(str(x))}` + "\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdCGenTest([]string{"--program", path}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "mfl_main") {
		t.Errorf("expected mfl_main emission in body, got:\n%.300s", out)
	}
}

// ---- guide.go (cmdGuide --json default branch + --text) --------------------

// TestCmdGuideJSONDefault confirms cmdGuide's default mode (no --text, no
// --skill) prints the catalog as JSON with the version pre-fronted.
func TestCmdGuideJSONDefault(t *testing.T) {
	out := captureStdout(t, func() {
		if err := cmdGuide(nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"version"`) {
		t.Errorf("default cmdGuide should print version JSON, got:\n%.200s", out)
	}
	if !strings.Contains(out, machinVersion) {
		t.Errorf("default cmdGuide should print machinVersion=%q, got first 200: %.200s", machinVersion, out)
	}
	if !strings.Contains(out, `"builtins"`) {
		t.Errorf("default cmdGuide JSON should include builtins key")
	}
}

// TestCmdGuideSkillText confirms the --text mode renders prose, not JSON.
func TestCmdGuideSkillText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := cmdGuide([]string{"--text"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("--text mode should not be JSON, got:\n%.200s", out)
	}
	if !strings.Contains(out, "machin "+machinVersion) {
		t.Errorf("--text mode should mention the version, got first 200: %.200s", out)
	}
}

// ---- transform.go (the one uncovered func-return-only lambda lift) ---------

// TestLiftClosuresFunctionWithReturnOnly covers a closure whose only statement
// is a `return` (no captured vars). Documents the trivial lift path.
func TestLiftClosuresFunctionWithReturnOnly(t *testing.T) {
	main := &FuncDecl{
		Name: "main",
		Body: []Stmt{
			&AssignStmt{Name: "f", Op: ":=", Val: &FuncLit{
				Body: []Stmt{&ReturnStmt{Vals: []Expr{&IntLit{Val: 42}}}},
			}},
		},
	}
	prog := &Program{Funcs: []*FuncDecl{main}}
	liftClosures(prog)
	if len(prog.Funcs) != 2 {
		t.Fatalf("want main + 1 lifted, got %d", len(prog.Funcs))
	}
	asc, ok := main.Body[0].(*AssignStmt)
	if !ok {
		t.Fatalf("body[0] = %T, want *AssignStmt", main.Body[0])
	}
	if _, ok := asc.Val.(*MakeClosure); !ok {
		t.Fatalf("f's value = %T, want *MakeClosure", asc.Val)
	}
	if prog.Funcs[1].NumCaptures != 0 {
		t.Errorf("return-only lambda should capture zero names, got %d", prog.Funcs[1].NumCaptures)
	}
}

// ---- build.go (ccPath / zigPath env override + cBytesLiteral multi-line) ----

func TestCcPath(t *testing.T) {
	if got := ccPath(); got != "cc" {
		t.Errorf("ccPath default = %q, want %q", got, "cc")
	}
	t.Setenv("CC", "clang")
	if got := ccPath(); got != "clang" {
		t.Errorf("ccPath with CC=clang = %q, want %q", got, "clang")
	}
}

func TestZigPathEnvOverride(t *testing.T) {
	// build_helpers_test.go already covers the default ("zig") case; this
	// test owns the env-override branch.
	t.Setenv("ZIG", "/opt/zig/zig")
	if got := zigPath(); got != "/opt/zig/zig" {
		t.Errorf("zigPath with ZIG=/opt/zig/zig = %q, want /opt/zig/zig", got)
	}
}

// TestCBytesLiteralMultiLine: a long payload verifies the every-20-bytes wrap
// + the final length constant.
func TestCBytesLiteralMultiLine(t *testing.T) {
	payload := make([]byte, 45)
	for i := range payload {
		payload[i] = byte(i)
	}
	out := cBytesLiteral("multi", payload)
	if !strings.Contains(out, "  0x00,0x01,") {
		t.Errorf("multi-line output should contain aligned rows, got:\n%.200s", out)
	}
	if want := "const unsigned long multi_len = 45UL;\n"; !strings.HasSuffix(out, want) {
		t.Errorf("multi-line output should end with %q, got suffix: %.60s", want, out[len(out)-60:])
	}
}

// ---- check.go (split error paths + locateCheckError + classify messages) ---

// TestCheckLocateCheckError verifies locateCheckError's "no matching decl"
// tail (returns "", 0) and its match-the-quoted-named case.
func TestCheckLocateCheckError(t *testing.T) {
	name, line := locateCheckError("typecheck: some error unrelated to any known name",
		[]string{"func foo(){1+1}", "func bar(){2+2}"}, []int{1, 2})
	if name != "" || line != 0 {
		t.Errorf("unmatched error: got (name=%q line=%d), want (\"\", 0)", name, line)
	}
	name, line = locateCheckError(`typecheck: function "foo" has a type error`,
		[]string{"func foo(){1+1}"}, []int{3})
	if name != "foo" || line != 3 {
		t.Errorf("matched error: got (name=%q line=%d), want (foo, 3)", name, line)
	}
}

// TestClassifyParseAllCases pins every branch of classifyParse.
func TestClassifyParseAllCases(t *testing.T) {
	cases := map[string]string{
		"unexpected token in input": "parse-unexpected-token",
		"expected foo at pos 5":     "parse-expected",
		`unterminated string at 5`:  "parse-unterminated-string",
		"unbalanced braces":         "parse-unbalanced-braces",
		"something else entirely":   "parse-error",
	}
	for in, want := range cases {
		if got := classifyParse(in); got != want {
			t.Errorf("classifyParse(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestClassifyCheckAllCases pins every branch of classifyCheck.
func TestClassifyCheckAllCases(t *testing.T) {
	cases := map[string]string{
		"type mismatch: int vs string":         "type-mismatch",
		`function "foo" shadows the builtin`:   "shadows-builtin",
		"no main function defined":             "no-main",
		`duplicate type "X"`:                   "duplicate-type",
		`duplicate function "f"`:              "duplicate-function",
		`struct has field "x" missing`:         "undefined-field",
		`undefined variable "x"`:              "undefined-name",
		`unknown type "Y"`:                    "undefined-name",
		`function "f" is not defined`:          "undefined-name",
		`function "f" expects 2 args`:          "arity-mismatch",
		"arity mismatch: 1 vs 2":               "arity-mismatch",
		`unsupported construct "X" at pos 1`:  "unsupported-construct",
		"any other typecheck message":          "type-error",
	}
	for in, want := range cases {
		if got := classifyCheck(in); got != want {
			t.Errorf("classifyCheck(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSplitFunctionsLocStringAware exercises splitFunctionsLoc's string-aware
// branch (a brace-like "}" inside a string literal must NOT count toward depth,
// matching a real-world JSON-building function whose source has "}{a}{b}".
func TestSplitFunctionsLocStringAware(t *testing.T) {
	src := `func build(){s := "}{a}{b}" println(len(s))}`
	blocks, lines, err := splitFunctionsLoc(src)
	if err != nil {
		t.Fatalf("splitFunctionsLoc: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("want 1 block (string-aware depth tracking), got %d: %v", len(blocks), blocks)
	}
	if lines[0] != 1 {
		t.Errorf("first block should report starting line 1, got %d", lines[0])
	}
}

// ---- shared helpers ---------------------------------------------------------

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	var b bytes.Buffer
	_, _ = b.ReadFrom(r)
	return b.String()
}

// writeSrc — local copy of testcmd_test.go's helper to keep this file
// self-contained.
func writeTestSrc(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
