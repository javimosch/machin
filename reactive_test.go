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

// path_index resolves a path to its registered index (and defaults to 0 for an
// unknown path); navigate(path_index(p)) is the by-path entry the host uses for
// link clicks and popstate.
func TestRouterPathIndex(t *testing.T) {
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
func page() (h) { h = str(current_route()) }
func main() {
    router_init(0)
    route("/")  route("/users")  route("/settings")
    outlet("outlet", func() { return page() })
    println("idx /users=" + str(path_index("/users")))
    println("idx /settings=" + str(path_index("/settings")))
    println("idx /missing=" + str(path_index("/missing")))
    navigate(path_index("/settings"))
    navigate(path_index("/"))
}`
	prog, perr := progFromSrcErr(runtime + host + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"idx /users=1",
		"idx /settings=2",
		"idx /missing=0", // unknown path defaults to the first route
		"OUT 2\nURL /settings",
		"OUT 0\nURL /",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// reactiveProg composes framework/reactive.src (extern block stripped, host funcs
// substituted) with an app, for the behavioral tests below.
func reactiveProg(t *testing.T, host, app string) *Program {
	t.Helper()
	data, err := os.ReadFile("framework/reactive.src")
	if err != nil {
		t.Skip("framework/reactive.src not found")
	}
	runtime := regexp.MustCompile(`(?s)extern "env" \{.*?\}\n`).ReplaceAllString(string(data), "")
	prog, perr := progFromSrcErr(runtime + host + app)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	return prog
}

// The three load-bearing reactivity guarantees, in one run:
//  1. change detection — set(id, sameValue) emits NO patch;
//  2. dependency isolation — only reactions that READ a signal re-run;
//  3. computed transitivity — set(base) updates a computed, which patches its slot.
func TestReactiveChangeAndIsolation(t *testing.T) {
	host := `func dom_patch(slot, val) { println("patch " + slot + "=" + val) }`
	app := `
var a = 0
var b = 0
func main() {
    a = signal(1)
    b = signal(100)
    bind("A", func() { return str(get(a)) })
    bind("B", func() { return str(get(b)) })
    c := computed(func() { return get(a) * 10 })
    bind("C", func() { return str(get(c)) })
    println("-- set a=1 (same) --")  set(a, 1)
    println("-- set a=2 --")          set(a, 2)
    println("-- set b=200 --")        set(b, 200)
}`
	out, err := RunCaptured(reactiveProg(t, host, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// initial paints
	for _, want := range []string{"patch A=1", "patch B=100", "patch C=10"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing initial %q in:\n%s", want, out)
		}
	}
	seg := func(from, to string) string {
		i := strings.Index(out, from)
		if to == "" {
			return out[i:]
		}
		return out[i:strings.Index(out, to)]
	}
	// (1) setting the same value patches nothing
	if same := seg("-- set a=1 (same) --", "-- set a=2 --"); strings.Contains(same, "patch") {
		t.Fatalf("set to an equal value must not patch; got:\n%s", same)
	}
	// (2)+(3) set a=2 patches A and the computed C, but NOT the independent B
	a2 := seg("-- set a=2 --", "-- set b=200 --")
	if !strings.Contains(a2, "patch A=2") || !strings.Contains(a2, "patch C=20") {
		t.Fatalf("set a=2 should patch A and computed C; got:\n%s", a2)
	}
	if strings.Contains(a2, "patch B") {
		t.Fatalf("set a must not touch B (dependency isolation); got:\n%s", a2)
	}
	// setting b touches only B
	b2 := seg("-- set b=200 --", "")
	if !strings.Contains(b2, "patch B=200") || strings.Contains(b2, "patch A") || strings.Contains(b2, "patch C") {
		t.Fatalf("set b should patch only B; got:\n%s", b2)
	}
}

// each emits MINIMAL list deltas: a removal is one list_remove (no re-insert), and a
// pure reorder is a single list_order with no insert/remove at all.
func TestReactiveEachRemoveReorder(t *testing.T) {
	host := `
func list_insert(c, k, h) { println("insert " + k) }
func list_remove(c, k) { println("remove " + k) }
func list_order(c, ks) { println("order " + ks) }`
	app := `
var ver = 0
var keys_csv = ""
func main() {
    ver = signal(0)
    each("L", func() { get(ver)  return keys_csv }, func(k) { return str(k) })
    println("-- 1,2,3 --")       keys_csv = "1,2,3"  set(ver, 1)
    println("-- remove 2 --")    keys_csv = "1,3"    set(ver, 2)
    println("-- reorder 3,1 --") keys_csv = "3,1"    set(ver, 3)
}`
	out, err := RunCaptured(reactiveProg(t, host, app))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rm := out[strings.Index(out, "-- remove 2 --"):strings.Index(out, "-- reorder 3,1 --")]
	if !strings.Contains(rm, "remove 2") || strings.Contains(rm, "insert") {
		t.Fatalf("removing a key should emit one remove and no insert; got:\n%s", rm)
	}
	if !strings.Contains(rm, "order 1,3") {
		t.Fatalf("removal should re-order to 1,3; got:\n%s", rm)
	}
	ro := out[strings.Index(out, "-- reorder 3,1 --"):]
	if strings.Contains(ro, "insert") || strings.Contains(ro, "remove") {
		t.Fatalf("a pure reorder must not insert/remove; got:\n%s", ro)
	}
	if !strings.Contains(ro, "order 3,1") {
		t.Fatalf("reorder should emit order 3,1; got:\n%s", ro)
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
