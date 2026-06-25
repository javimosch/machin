package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A user function that shadows a builtin is rejected (it would be silently ignored
// at call sites otherwise — the footgun that bit the reactive runtime three times).
func TestBuiltinShadowingRejected(t *testing.T) {
	for _, name := range []string{"flush", "keys", "contains", "len", "str"} {
		src := "func " + name + "() { println(1) }\nfunc main() { " + name + "() }"
		fn := strings.SplitN(src, "\n", 2)
		blocks := []string{normalize(fn[0]), normalize(fn[1])}
		prog, perr := ParseProgram(blocks)
		if perr != nil {
			t.Fatalf("%s: parse: %v", name, perr)
		}
		if _, err := Check(prog); err == nil || !strings.Contains(err.Error(), "shadows the builtin") {
			t.Fatalf("defining func %q should be rejected as shadowing a builtin, got %v", name, err)
		}
	}
}

// End-to-end behavior: compose the reactive runtime with print-based host functions
// and a list app, run it natively, and assert the patches are MINIMAL — adding an
// item inserts only that item, and a reorder emits no item re-render.
func TestReactiveMinimalPatches(t *testing.T) {
	data, err := os.ReadFile("framework/reactive.src")
	if err != nil {
		t.Skip("framework/reactive.src not found")
	}
	// strip the extern block; substitute printing host functions.
	runtime := regexp.MustCompile(`(?s)extern "env" \{.*?\}\n`).ReplaceAllString(string(data), "")
	host := `
func dom_patch(slot, val) { println("patch " + slot + "=" + val) }
func list_insert(c, k, html) { println("insert " + k) }
func list_remove(c, k) { println("remove " + k) }
func list_order(c, ks) { println("order " + ks) }`
	app := `
var ver = 0
var ids = []int{}
var vals = []int{}
var n = 0
var nid = 1
func total_of() (s) { get(ver)  s = 0  i := 0  while i < n { s = s + vals[i]  i = i + 1 } }
func add(x) { ids = append(ids, nid)  vals = append(vals, x)  nid = nid + 1  n = n + 1  set(ver, get(ver) + 1) }
func main() {
    ver = signal(0)
    bind("total", func() { return str(total_of()) })
    each("list", func() { get(ver)  return csv(ids) }, func(k) { return str(k) })
    println("-- add 10 --")  add(10)
    println("-- add 20 --")  add(20)
}`
	prog, perr := progFromSrcErr(runtime + host + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// after `add 20`, exactly one insert (#2) — item #1 must NOT be re-inserted.
	after := out[strings.Index(out, "-- add 20 --"):]
	if strings.Count(after, "insert") != 1 {
		t.Fatalf("adding a 2nd item should insert only it; got:\n%s", out)
	}
	if !strings.Contains(after, "insert 2") || strings.Contains(after, "insert 1") {
		t.Fatalf("expected only `insert 2` after the 2nd add; got:\n%s", out)
	}
	if !strings.Contains(after, "patch total=30") {
		t.Fatalf("computed total should update to 30; got:\n%s", out)
	}
}

// Templating: a component declares its markup AND bindings in one `mount` call —
// `slot`/`list` return markup and queue their reaction; `mount` emits the HTML once
// then flushes the queue (so the slots paint after they exist in the DOM).
func TestReactiveTemplating(t *testing.T) {
	data, err := os.ReadFile("framework/reactive.src")
	if err != nil {
		t.Skip("framework/reactive.src not found")
	}
	runtime := regexp.MustCompile(`(?s)extern "env" \{.*?\}\n`).ReplaceAllString(string(data), "")
	host := `
func dom_mount(root, html) { println("MOUNT " + html) }
func dom_patch(slot, val) { println("patch " + slot + "=" + val) }
func list_insert(c, k, h) { println("insert " + k) }
func list_remove(c, k) { println("remove " + k) }
func list_order(c, ks) { println("order " + ks) }`
	app := `
var ver = 0
var ids = []int{}
var n = 0
var nid = 1
func add() { ids = append(ids, nid)  nid = nid + 1  n = n + 1  set(ver, get(ver) + 1) }
func main() {
    ver = signal(0)
    mount("app",
        "<h1>t</h1>" + slot("count", func() { get(ver)  return str(n) }) +
        list("items", func() { get(ver)  return csv(ids) }, func(id) { return str(id) }))
    println("-- add --")  add()
}`
	prog, perr := progFromSrcErr(runtime + host + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// the component generated its own HTML skeleton (a slot span + a list container)
	if !strings.Contains(out, `data-s="count"`) || !strings.Contains(out, `id="items"`) {
		t.Fatalf("mount did not emit the declared slots; got:\n%s", out)
	}
	// the queued bindings flushed (painted) after mount, and add() patched them
	if !strings.Contains(out, "patch count=0") {
		t.Fatalf("queued binding did not paint on mount; got:\n%s", out)
	}
	after := out[strings.Index(out, "-- add --"):]
	if !strings.Contains(after, "patch count=1") || !strings.Contains(after, "insert 1") {
		t.Fatalf("add() should patch count and insert the item; got:\n%s", out)
	}
}

// hydrate activates bindings against an already-rendered (SSR) DOM: it flushes the
// queued slots WITHOUT a dom_mount, and a value-embedding `slot` means the markup
// the component builds already carries the value.
func TestReactiveHydrate(t *testing.T) {
	data, err := os.ReadFile("framework/reactive.src")
	if err != nil {
		t.Skip("framework/reactive.src not found")
	}
	runtime := regexp.MustCompile(`(?s)extern "env" \{.*?\}\n`).ReplaceAllString(string(data), "")
	host := `
func dom_mount(root, html) { println("MOUNT") }
func dom_patch(slot, val) { println("patch " + slot + "=" + val) }
func list_insert(c, k, h) {}
func list_remove(c, k) {}
func list_order(c, ks) {}`
	app := `
var c = 0
func main() {
    c = signal(7)
    m := slot("v", func() { return str(get(c)) })   // markup carries the value
    println("markup=" + m)
    hydrate(m)
    set(c, 9)
}`
	prog, perr := progFromSrcErr(runtime + host + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out, "MOUNT") {
		t.Fatalf("hydrate must not call dom_mount; got:\n%s", out)
	}
	if !strings.Contains(out, `data-s="v">7</span>`) {
		t.Fatalf("slot should embed its initial value (7); got:\n%s", out)
	}
	if !strings.Contains(out, "patch v=9") {
		t.Fatalf("the hydrated binding should react to set(c, 9); got:\n%s", out)
	}
}

// The router (framework/router.src, on top of reactive) switches pages by route
// index: navigate() re-renders the outlet (via dom_html) and syncs the URL.
func TestRouter(t *testing.T) {
	rv, err := os.ReadFile("framework/reactive.src")
	if err != nil {
		t.Skip("framework/reactive.src not found")
	}
	rt, err := os.ReadFile("framework/router.src")
	if err != nil {
		t.Skip("framework/router.src not found")
	}
	strip := regexp.MustCompile(`(?s)extern "env" \{.*?\}\n`)
	runtime := strip.ReplaceAllString(string(rv), "") + strip.ReplaceAllString(string(rt), "")
	host := `
func dom_mount(r, h) {}
func dom_patch(s, v) {}
func list_insert(c, k, h) {}
func list_remove(c, k) {}
func list_order(c, ks) {}
func dom_html(id, html) { println("OUT " + html) }
func nav_url(path) { println("URL " + path) }`
	app := `
func page() (h) { r := current_route()  h = "?"  if r == 0 { h = "home" }  if r == 1 { h = "users" } }
func main() {
    router_init(0)
    route("/")  route("/users")
    outlet("outlet", func() { return page() })
    navigate(1)
    navigate(0)
}`
	prog, perr := progFromSrcErr(runtime + host + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "OUT home\nOUT users\nURL /users\nOUT home\nURL /\n"
	if out != want {
		t.Fatalf("router output:\n%q\nwant:\n%q", out, want)
	}
}

// progFromSrcErr is like progFromSrc but returns the error (for table tests).
func progFromSrcErr(src string) (*Program, error) {
	blocks, err := splitFunctions(src)
	if err != nil {
		return nil, err
	}
	var decls []string
	for _, b := range blocks {
		decls = append(decls, normalize(b))
	}
	return ParseProgram(decls)
}
