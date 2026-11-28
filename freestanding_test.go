package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func freestandingProg(t *testing.T, src string) *Program {
	t.Helper()
	blocks, err := splitFunctions(src)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var decls []string
	for _, b := range blocks {
		decls = append(decls, normalize(b))
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return prog
}

// The emitted C is the program and its inline helpers: no libc header, no
// constructor, no runtime.
func TestFreestandingEmitsNoRuntime(t *testing.T) {
	prog := freestandingProg(t, `
var count = -1
var ratio = 0.5
var on = true

func pair(a) { return a, a + 1 }

export func bump(p, d) {
	x, y := pair(peek_u32(p, 0))
	for count < d { count = count + 1 }
	if on && ratio < 1.0 { poke_i64(p, 8, ushr(x + y, 1)) }
	return count
}
`)
	c, exports, err := CompileToCTarget(prog, false, targetWasmFreestanding)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !reflect.DeepEqual(exports, []string{"bump"}) {
		t.Fatalf("exports = %v", exports)
	}
	if n := strings.Count(c, "#include"); n != 1 || !strings.Contains(c, "#include <stdint.h>") {
		t.Fatalf("want only stdint.h, got %d includes:\n%s", n, c)
	}
	for _, banned := range []string{"constructor", "malloc", "memset", "printf", "pthread", "mfl_alloc", "mfl_rr_"} {
		if strings.Contains(c, banned) {
			t.Errorf("output mentions %q", banned)
		}
	}
	for _, want := range []string{
		"static int64_t mfl_g_count = ", "static double mfl_g_ratio = ",
		`__attribute__((export_name("bump"))) int64_t bump(int64_t a0, int64_t a1)`,
		"static inline int64_t mfl_peek_u32(", "static inline int64_t mfl_ushr(", "static inline void mfl_poke_i64(",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in:\n%s", want, c)
		}
	}
}

// Everything that needs the runtime is refused by name.
func TestFreestandingRejects(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"string literal", `export func f() { println("hi") return 1 }`, "println is not available"},
		{"string variable", `export func f() { s := "x" return 1 }`, "variable s"},
		{"string operand", `export func f(n) { if "a" == "b" { return n } return 1 }`, "string literals"},
		{"str builtin", `export func f(n) { x := len(str(n)) return x }`, "len is not available"},
		{"libm", `export func f(x) { return int(sqrt(float(x))) }`, "sqrt is not available"},
		{"alloc", `export func f() { return alloc(8) }`, "alloc is not available"},
		{"slice", `export func f() { s := []int{1} return s[0] }`, "variable s"},
		{"slice temporary", `export func f(i) { return []int{1, 2}[i] }`, "not supported"},
		{"map", `export func f() { m := make(map[int]int) m[1] = 2 return 1 }`, "variable m"},
		{"channel", `export func f() { c := make(chan int) c <- 1 return 1 }`, "variable c"},
		{"goroutine", `func g() { }` + "\n" + `export func f() { go g() return 1 }`, "go statement"},
		{"arena", `export func f() { x := 0 arena { x = 1 } return x }`, "arena statement"},
		{"closure", `export func f(n) { g := func() { return n } return g() }`, "closures"},
		{"struct", `type P struct { x int }` + "\n" + `export func f() { return 1 }`, "structs"},
		{"string param to host", `extern "env" { fn log(string) }` + "\n" + `export func f() { return 1 }`, `type "string"`},
		{"link", `extern "m" { link "m" fn g(i64) i64 }` + "\n" + `export func f() { return g(1) }`, "`link`"},
		{"computed global", `func one() { return 1 }` + "\n" + `var g = one()` + "\n" + `export func f() { return g }`, "literal"},
		{"string global", `var g = "x"` + "\n" + `export func f() { return 1 }`, "literal"},
		{"no export", `func main() { x := 1 x = x + 1 }`, "no exported functions"},
		{"main", `func main() { }` + "\n" + `export func f() { return 1 }`, "entry point"},
		{"multi-result export", `export func f() { return 1, 2 }`, "at most one value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := CompileToCTarget(freestandingProg(t, tc.src), false, targetWasmFreestanding)
			if err == nil {
				t.Fatalf("compiled; want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), "wasm-freestanding:") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q, want wasm-freestanding: ... %q", err, tc.want)
			}
		})
	}
	if _, _, err := CompileToCTarget(freestandingProg(t, `export func f() { return 1 }`), true, targetWasmFreestanding); err == nil {
		t.Fatal("--safe was accepted")
	}
}

// Every peek/poke the allow-list accepts has to exist and compile.
func TestFreestandingAllowListMatchesPrelude(t *testing.T) {
	prelude := freestandingPrelude()
	n := 0
	for _, op := range []string{"peek", "poke"} {
		for _, w := range []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64", "ptr"} {
			name := op + "_" + w
			if !freestandingBuiltin(name) || !strings.Contains(prelude, " mfl_"+name+"(") {
				t.Errorf("%s is not available", name)
			}
			n++
		}
	}
	if got := strings.Count(prelude, "static inline "); got != n+5 { // + the two f64 bit casts, ushr, << and >>
		t.Errorf("prelude has %d helpers, want %d", got, n+3)
	}
	if freestandingBuiltin("peek_bogus") {
		t.Error("peek_bogus accepted")
	}
}

// The point of the target: an MFL object linked into a freestanding C program,
// calls in both directions, state kept across calls, and no import beyond the
// one the MFL source declares. See testdata/freestanding.
func TestFreestandingLinksIntoC(t *testing.T) {
	if _, err := exec.LookPath(zigPath()); err != nil {
		t.Skip("zig not installed")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src := filepath.Join("testdata", "freestanding")
	prog := freestandingProg(t, readFileOrFatal(t, filepath.Join(src, "core.src")))
	dir := t.TempDir()
	obj, wasm := filepath.Join(dir, "core.o"), filepath.Join(dir, "prog.wasm")
	if err := BuildWasmFreestanding(prog, obj, []string{"-I" + src}); err != nil {
		t.Fatalf("build object: %v", err)
	}
	link := []string{"cc", "-target", "wasm32-freestanding", "-ffreestanding", "-fno-builtin", "-nostdlib",
		"-O2", "-fwrapv", "-Wl,--no-entry", "-I" + src, filepath.Join(src, "host.c"), obj, "-o", wasm}
	if out, err := exec.Command(zigPath(), link...).CombinedOutput(); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	out, err := exec.Command(node, filepath.Join(src, "run.mjs"), wasm).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	var want map[string]any
	const expected = `{"imports":["env.js_trace"],"boot":5050,"slices":45,"faultAt":41,"retired":308,"retiredDirect":308,"floats":1,"traces":[305,307]}`
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %s\nwant %s", out, expected)
	}
}

func readFileOrFatal(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
