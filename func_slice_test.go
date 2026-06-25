package main

import (
	"os"
	"strings"
	"testing"
)

// A slice of functions — `[]func{}` — can be built, appended to, indexed, and its
// elements called. This is the dispatch-table / effect-list primitive (it drove
// the reactive runtime in framework/reactive.src).
func TestFuncSliceStoreAndCall(t *testing.T) {
	prog := progFromSrc(t, `
func add_to(fns, f) (out) { out = append(fns, f) }
func main() {
    a := 3
    b := 10
    fns := []func{}
    fns = append(fns, func() { return str(a * a) })
    fns = add_to(fns, func() { return str(b + 1) })
    i := 0
    while i < len(fns) {
        f := fns[i]
        println(f())
        i = i + 1
    }
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Fields(out); len(got) != 2 || got[0] != "9" || got[1] != "11" {
		t.Fatalf("func slice calls = %q (want 9 11)", out)
	}
}

// `[]func` is a slice of closures (mfl_closure elements), passed by value like any
// slice — the element type resolves to mfl_closure in the emitted C.
func TestFuncSliceCodegen(t *testing.T) {
	prog := progFromSrc(t, "func main() { fns := []func{}  fns = append(fns, func() { return 1 })  f := fns[0]  println(f()) }")
	csrc, err := CompileToC(prog, false)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(csrc, "mfl_closure") {
		t.Fatal("func slice did not lower to mfl_closure elements")
	}
}

// The reactive runtime module composes and type-checks (its signal/get/set/bind
// over a []func binding registry is the headline use of func slices).
func TestReactiveFrameworkCompiles(t *testing.T) {
	data, err := os.ReadFile("framework/reactive.src")
	if err != nil {
		t.Skip("framework/reactive.src not found")
	}
	runtime := string(data)
	// exercise the full surface: a signal, a computed, a binding, and a keyed list.
	app := `
var c = 0
var dbl = 0
var ids = []int{}
export func start() {
    c = signal(0)
    dbl = computed(func() { return get(c) * 2 })
    bind("count", func() { return str(get(c)) })
    bind("double", func() { return str(get(dbl)) })
    each("list", func() { get(c)  return csv(ids) }, func(k) { return str(k) })
}
export func bump(d) { set(c, get(c) + d) }`
	prog := progFromSrc(t, runtime+"\n"+app)
	if _, _, err := CompileToCTarget(prog, false, targetWasm); err != nil {
		t.Fatalf("reactive runtime failed to compile for wasm: %v", err)
	}
}

// The runtime also compiles for an app that uses signals/bindings but NO list —
// the keys closure's string return keeps `each` type-checkable even when unused.
func TestReactiveNoListCompiles(t *testing.T) {
	data, err := os.ReadFile("framework/reactive.src")
	if err != nil {
		t.Skip("framework/reactive.src not found")
	}
	app := `
var c = 0
export func start() { c = signal(0)  bind("count", func() { return str(get(c)) }) }
export func bump(d) { set(c, get(c) + d) }`
	prog := progFromSrc(t, string(data)+"\n"+app)
	if _, _, err := CompileToCTarget(prog, false, targetWasm); err != nil {
		t.Fatalf("reactive runtime (no list) failed to compile: %v", err)
	}
}
