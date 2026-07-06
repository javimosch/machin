package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
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

// TestParseIntAndNowMs covers parse_int (string -> int, 0 on garbage) and now_ms
// (monotone-ish wall-clock ms) — surfaced while building a real CLI tool.
func TestParseIntAndNowMs(t *testing.T) {
	got := runNative(t, `func main(){ println(parse_int("8080")) println(parse_int("x")) println(parse_int("-42")) a := now_ms() sleep(20) println((now_ms() - a) >= 15) }`)
	if want := "8080\n0\n-42\ntrue\n"; got != want {
		t.Fatalf("parse_int/now_ms: got %q, want %q", got, want)
	}
}

// TestParseIntEmptyString covers parse_int("") edge case — empty input should return 0.
func TestParseIntEmptyString(t *testing.T) {
	got := runNative(t, `func main(){ println(parse_int("")) }`)
	if want := "0\n"; got != want {
		t.Fatalf("parse_int empty string: got %q, want %q", got, want)
	}
}

// TestStringLiteralPunct guards against a parser bug surfaced building the SSG:
// a string literal whose value is a structural token (")", "}", ",") was
// mistaken for that token, terminating an argument/element list early.
func TestStringLiteralPunct(t *testing.T) {
	got := runNative(t, `func main(){ println(index("a)b", ")")) xs := []string{"}", ","} println(len(xs)) println(xs[0]) }`)
	if want := "1\n2\n}\n"; got != want {
		t.Fatalf("string-literal punct: got %q, want %q", got, want)
	}
}

// TestFileIO covers write_file/read_file/list_dir/mkdir — surfaced building a
// static-site generator.
func TestFileIO(t *testing.T) {
	dir := t.TempDir()
	src := `func main(){ mkdir("` + dir + `/sub") write_file("` + dir + `/sub/a.txt", "hi there") println(read_file("` + dir + `/sub/a.txt")) println(len(list_dir("` + dir + `/sub"))) println(read_file("` + dir + `/missing")) }`
	got := runNative(t, src)
	if want := "hi there\n1\n\n"; got != want {
		t.Fatalf("file I/O: got %q, want %q", got, want)
	}
}

// read_file/read_file_bytes on a directory path used to SEGFAULT: fopen(dir,
// "rb") succeeds on Linux, but ftell() on it returns LONG_MAX (not -1), so
// the "if (n < 0) n = 0" guard never caught it and the runtime tried to
// alloc ~9.2 exabytes. This is an easy real path to hit — list_dir()'s
// entries can themselves be directories — surfaced building a concurrent
// file-hasher demo (`machin-hasher`) that crashed on any directory
// containing so much as one subdirectory. Both must now return empty
// (matching the existing "can't open" behavior), not crash.
func TestReadFileOnDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(dir+"/sub", 0o755); err != nil {
		t.Fatal(err)
	}
	src := `func main(){
	println("[" + read_file("` + dir + `/sub") + "]")
	b := read_file_bytes("` + dir + `/sub")
	println(str(len(b)))
}`
	got := runNative(t, src)
	if want := "[]\n0\n"; got != want {
		t.Fatalf("read_file(_bytes) on a directory: got %q, want %q (must not crash)", got, want)
	}
}

// TestRemove covers remove(path): 0 on success (file actually gone), -1 on a
// missing path. Had no test coverage anywhere in the suite.
func TestRemove(t *testing.T) {
	dir := t.TempDir()
	src := `func main(){
	write_file("` + dir + `/a.txt", "bye")
	println(str(remove("` + dir + `/a.txt")))
	println(read_file("` + dir + `/a.txt"))
	println(str(remove("` + dir + `/missing.txt")))
}`
	got := runNative(t, src)
	if want := "0\n\n-1\n"; got != want {
		t.Fatalf("remove: got %q, want %q", got, want)
	}
}

// loadMFL (used by `machin run`/`build` on a .mfl FILE) used to only accept
// canonical one-declaration-per-line text or the packed base64 form, so a
// hand-written .mfl with ordinary Go-like multi-line formatting failed with a
// misleading "line is neither plain MFL nor base64" error — a line like
// `println(x)` has no whitespace, so it looked like a malformed packed
// declaration rather than a fragment of a multi-line function body. `machin
// check` already tolerated this shape (it uses the same encode-style
// splitFunctionsLoc/normalize machinery); this closes the inconsistency.
// Surfaced independently by 3 of 5 dogfood agents in one session (2026-07).
func TestLoadMFLAcceptsLooseSource(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/loose.mfl"
	loose := "func main() {\n" +
		"\tx := 5\n" +
		"\tif x < 10 {\n" +
		"\t\tprintln(\"hi\")\n" +
		"\t}\n" +
		"}\n"
	if err := os.WriteFile(path, []byte(loose), 0o644); err != nil {
		t.Fatal(err)
	}
	prog, err := loadMFL(path)
	if err != nil {
		t.Fatalf("loadMFL on loose multi-line source: %v", err)
	}
	if len(prog.Funcs) != 1 || prog.Funcs[0].Name != "main" {
		t.Fatalf("expected one main() func, got %+v", prog.Funcs)
	}

	bin, err := os.CreateTemp("", "mfl-loose-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build from loose-source-loaded program: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(out) != "hi\n" {
		t.Fatalf("got %q, want \"hi\\n\"", out)
	}
}

// Non-regression: loadMFL must still accept canonical (one decl/line) and
// packed (base64) .mfl files exactly as before.
func TestLoadMFLStillAcceptsCanonicalAndPacked(t *testing.T) {
	dir := t.TempDir()

	canonical := dir + "/canon.mfl"
	if err := os.WriteFile(canonical, []byte(`func main(){println("canon")}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if prog, err := loadMFL(canonical); err != nil || len(prog.Funcs) != 1 {
		t.Fatalf("canonical .mfl: prog=%+v err=%v", prog, err)
	}

	packed := dir + "/packed.mfl"
	enc := base64.StdEncoding.EncodeToString([]byte(`func main(){println("packed")}`))
	if err := os.WriteFile(packed, []byte(enc+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if prog, err := loadMFL(packed); err != nil || len(prog.Funcs) != 1 {
		t.Fatalf("packed .mfl: prog=%+v err=%v", prog, err)
	}
}

// loadMFL's three error-wrapping branches (bad packed data, a parse error,
// and a file with no functions) were only ever exercised indirectly through
// loadDecls/ParseProgram's own tests, never through loadMFL itself — so a
// regression that dropped the `%s: %w` path prefix would have gone unnoticed.
func TestLoadMFLErrorPaths(t *testing.T) {
	dir := t.TempDir()

	badPacked := dir + "/bad.mfl"
	if err := os.WriteFile(badPacked, []byte("not-base64!!!\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMFL(badPacked); err == nil || !strings.Contains(err.Error(), badPacked) {
		t.Fatalf("loadMFL(bad packed) error = %v, want it to mention %q", err, badPacked)
	}

	badSyntax := dir + "/syntax.mfl"
	if err := os.WriteFile(badSyntax, []byte("func main() { if }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMFL(badSyntax); err == nil || !strings.Contains(err.Error(), badSyntax) {
		t.Fatalf("loadMFL(bad syntax) error = %v, want it to mention %q", err, badSyntax)
	}

	empty := dir + "/empty.mfl"
	if err := os.WriteFile(empty, []byte("   \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMFL(empty); err == nil || !strings.Contains(err.Error(), "no functions") {
		t.Fatalf("loadMFL(empty) error = %v, want \"no functions\"", err)
	}
}

// TestEnvMissingVariable covers env() with a nonexistent environment variable — should return empty string.
func TestEnvMissingVariable(t *testing.T) {
	got := runNative(t, `func main(){ println("[" + env("THIS_VAR_DOES_NOT_EXIST_XYZ") + "]") }`)
	if want := "[]\n"; got != want {
		t.Fatalf("env missing variable: got %q, want %q", got, want)
	}
}
