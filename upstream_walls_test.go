package main

// Tests for the four walls a JS->MFL transpiler hit (#710-#713).

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// walls runs `encode` over loose source and returns the checked program.
func wallsCompose(t *testing.T, src string) (*Program, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.src")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	prog, _, err := composeSources([]string{p})
	return prog, err
}

func TestFunctionNameAsValue(t *testing.T) {
	prog, err := wallsCompose(t, `
var hits = 0
func double(x) (r) { r = x * 2 }
func less(a, b) { return a < b }
func note(x) { hits = hits + x }
func apply(f, x) (r) { r = f(x) }
func each(xs, f) { for _, x := range xs { f(x) } }
func pick() { return double }
func fact(n) (r) { r = 1  if n > 1 { f := fact  r = n * f(n - 1) } }
func shout(s) (r) { r = s + "!" }
func ident(x) { return x }
func feed(c) { c <- 20 }
func main() {
    println(apply(double, 4))
    ys := sort_by([]int{3, 1, 2}, less)
    println(ys[0], ys[2])
    each(ys, note)
    println(hits)
    g := pick()
    println(g(5), fact(5))
    println(apply(shout, "a"), apply(ident, "s"), apply(ident, 3))
    w := func() { return apply(double, 1) }
    println(w())
    ch := make(chan int)
    go feed(ch)
    select {
    case got := <-ch:
        k := func() { return apply(double, got) + 1 }
        println(k())
    }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "prog")
	if err := BuildBinary(prog, bin, true); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := "8\n1 3\n6\n10 120\na! s 3\n2\n41\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

// A local, a param or a global of the same name is the variable, never the function.
func TestFunctionNameShadowedByVariable(t *testing.T) {
	for name, src := range map[string]string{
		"local":  "func n() { return 1 }\nfunc main() { n := 5  println(n + 1) }\n",
		"param":  "func n() { return 1 }\nfunc f(n) { return n + 1 }\nfunc main() { println(f(5)) }\n",
		"select": "func n() { return 1 }\nfunc feed(c) { c <- 5 }\nfunc main() { c := make(chan int)  go feed(c)\n select {\n case n := <-c:\n println(n + 1)\n }\n }\n",
	} {
		if _, err := wallsCompose(t, src); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestFunctionValueDiagnostics(t *testing.T) {
	for src, want := range map[string]string{
		"func two() { return 1, 2 }\nfunc main() { f := two }\n":            "returns more than one value; wrap it in a func literal",
		"func main() { f := len }\n":                                        `"len" is a builtin, not a value; wrap it`,
		"func d(x) { return x }\nvar g = d\nfunc main() { }\n":              "global initializer cannot name it",
		"func d() { return 1 }\nfunc main() { x := d  d := 2 }\n":           "used before the local of the same name",
		"func main() { println(nope) }\n":                                   `undefined variable "nope"`,
		"extern \"c\" { fn puts(string) int }\nfunc main() { f := puts }\n": "is an extern function, not a value",
	} {
		_, err := wallsCompose(t, src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want it to mention %q", src, err, want)
		}
	}
}

func TestFuncLiteralInSelectCase(t *testing.T) {
	if _, err := wallsCompose(t, "func feed(c) { c <- 1 }\nfunc main() { c := make(chan int)  go feed(c)  base := 2\n select {\n case v := <-c:\n f := func(x) { return x + v + base }\n println(f(1))\n }\n }\n"); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeNoCheckLibraryFile(t *testing.T) {
	lib := filepath.Join(t.TempDir(), "lib.src")
	// no main, a call and a struct type that are defined in some other module
	if err := os.WriteFile(lib, []byte("// doc\nfunc mk() (p) {\n    p = Elsewhere{x: helper(1, 2)}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdEncode([]string{lib}); err == nil {
		t.Fatal("default encode must still reject a library fragment")
	}
	cs, err := canonicalize([]string{lib})
	if err != nil {
		t.Fatal(err)
	}
	if want := "func mk()(p){p=Elsewhere{x:helper(1,2)}}\n\n"; cs.text != want {
		t.Fatalf("got %q, want %q", cs.text, want)
	}
	if err := cmdEncode([]string{"--no-check", lib}); err != nil {
		t.Fatal(err)
	}
	if err := cmdEncode([]string{"--no-chek", lib}); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("a mistyped flag must be rejected, got %v", err)
	}
}

func TestStaticLinkArgs(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "libvt.a")
	if err := os.WriteFile(archive, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{"-L" + dir}, {"-L", dir}} {
		got, err := staticLinkArgs([]string{"-lvt", "-lm"}, flags, []string{"vt"})
		if err != nil || strings.Join(got, " ") != archive+" -lm" {
			t.Fatalf("%v: got %v, %v", flags, got, err)
		}
	}
	if _, err := staticLinkArgs([]string{"-lvt"}, nil, []string{"machin_no_such_lib"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing archive: %v", err)
	}
	if _, err := staticLinkArgs([]string{"-lm"}, []string{"-L" + dir}, []string{"vt"}); err == nil || !strings.Contains(err.Error(), "does not link") {
		t.Fatalf("unlinked name: %v", err)
	}
}

// End to end: a vendored library outside the system paths, with no path in the source.
func TestBuildWithVendoredLibrary(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("vt.h", "int vt_val(void);\n")
	c := write("vt.c", "int vt_val(void){return 42;}\n")
	obj := filepath.Join(dir, "vt.o")
	for _, cmd := range []*exec.Cmd{
		exec.Command(ccPath(), "-c", c, "-o", obj),
		exec.Command("ar", "rcs", filepath.Join(dir, "libvt.a"), obj),
	} {
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("no C toolchain for the fixture: %v %s", err, out)
		}
	}
	src := write("app.src", "extern \"vt\" {\n header \"vt.h\"\n link \"vt\"\n fn vt_val() int\n}\nfunc main() { println(vt_val()) }\n")
	_, text, err := composeSources([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	mfl, bin := write("app.mfl", text), filepath.Join(dir, "app")
	if err := cmdBuild([]string{mfl, "-o", bin, "--cflags", "-I" + dir, "--ldflags", "-L" + dir, "--static-link", "vt"}); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin).Output(); err != nil || string(out) != "42\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	for _, bad := range [][]string{
		{mfl, "--cflags"},
		{mfl, "--no-such-flag"},
		{mfl, "--target", "wasm", "--cflags", "-O1"},
		{mfl, "--emit-c", "--static-link", "vt"},
	} {
		if err := cmdBuild(bad); err == nil {
			t.Errorf("cmdBuild(%v) succeeded", bad)
		}
	}
}

func TestParseGuideSig(t *testing.T) {
	for sig, want := range map[string]string{
		"(...) ->":                         `{"params":[{"type":"any"}],"variadic":true,"returns":[]}`,
		"() -> string":                     `{"params":[],"returns":["string"]}`,
		"(int, string[, []string]) -> int": `{"params":[{"type":"int"},{"type":"string"},{"type":"[]string","optional":true}],"returns":["int"]}`,
		"([]T, func(T, T) bool) -> []T":    `{"params":[{"type":"[]T"},{"type":"func(T, T) bool"}],"returns":["[]T"]}`,
		"(int ob, float f) -> void":        `{"params":[{"name":"ob","type":"int"},{"name":"f","type":"float"}],"returns":[]}`,
		"(int) -> (bytes, map[string]int)": `{"params":[{"type":"int"}],"returns":["bytes","map[string]int"]}`,
	} {
		got, err := parseGuideSig(sig)
		if err != nil {
			t.Errorf("%s: %v", sig, err)
			continue
		}
		if b, _ := json.Marshal(got); string(b) != want {
			t.Errorf("%s: got %s, want %s", sig, b, want)
		}
	}
	for _, bad := range []string{"int -> int", "(int -> int", "(int) int", "(int[, int], int) -> int"} {
		if _, err := parseGuideSig(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// Every catalog entry must have a structured form: a new free-text sig that the
// grammar cannot read fails here, not in a consumer.
func TestEveryBuiltinSigIsStructured(t *testing.T) {
	for _, b := range machinGuide().Builtins {
		sig, err := parseGuideSig(b.Sig)
		if err != nil {
			t.Errorf("%s: %v", b.Name, err)
			continue
		}
		for _, p := range sig.Params {
			if p.Type == "" || strings.ContainsAny(p.Type, "[") && strings.Contains(p.Type, "[,") {
				t.Errorf("%s: bad param type %q", b.Name, p.Type)
			}
		}
	}
	if _, err := json.Marshal(machinGuide()); err != nil {
		t.Fatal(err)
	}
}

func TestGuideFrameworks(t *testing.T) {
	// These cannot be typed without a calling program (`check --all` fails on
	// them too); they are listed with "any". Shrink this, never grow it silently.
	untyped := map[string]bool{"flags": true, "httpstream": true, "redis": true, "wsclient": true}
	for _, name := range guideFrameworks() {
		c, err := describeFramework(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if c.Typed == untyped[name] {
			t.Errorf("%s: typed=%v (%s)", name, c.Typed, c.TypeError)
		}
		if len(c.Functions) == 0 {
			t.Errorf("%s: no functions", name)
		}
	}
	web, err := describeFramework("machweb")
	if err != nil {
		t.Fatal(err)
	}
	sigs := map[string]string{}
	for _, f := range web.Functions {
		sigs[f.Name] = f.Sig
	}
	for name, want := range map[string]string{
		"ok_html":   "(body string) -> Response",
		"not_found": "() -> Response",
		"redirect":  "(url string) -> Response",
		"serve":     "(port int, handler func(Request) Response) ->",
	} {
		if sigs[name] != want {
			t.Errorf("%s: got %q, want %q", name, sigs[name], want)
		}
	}
	if len(web.Types) == 0 || web.Types[0].Name != "Request" {
		t.Errorf("machweb types = %+v", web.Types)
	}
	if err := cmdGuide([]string{"--framework", "machweb"}); err != nil {
		t.Error(err)
	}
	if err := cmdGuide([]string{"--framework", "sso", "--text"}); err != nil {
		t.Error(err)
	}
	for _, bad := range [][]string{{"--framework"}, {"--framework", "nope"}, {"--framework", "example"}} {
		if err := cmdGuide(bad); err == nil {
			t.Errorf("cmdGuide(%v) succeeded", bad)
		}
	}
}
