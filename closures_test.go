package main

import (
	"strings"
	"testing"
)

// A captured closure variable can now be CALLED inside a lambda — `func(){ fn() }`
// where fn is captured. Previously the call resolved as a named-function call and
// failed ("undefined function"), because freeIdents ignored a Call's callee, so the
// closure was never captured. This is the higher-order-function building block.
func TestCapturedClosureCalledInLambda(t *testing.T) {
	cases := []struct{ src, want string }{
		// return a closure that calls its captured argument
		{`func wrap(fn) (g) { g = func() { return fn() } }
		  func main() { h := wrap(func() { return 7 })  println(h()) }`, "7"},
		// captured closure called with an argument, inside a nested lambda
		{`func g(h) (r) { r = func() { return h(5) } }
		  func main() { sq := g(func(x) { return x * x })  println(sq()) }`, "25"},
		// the reactive shape: a stored func() thunk that calls a captured compute
		{`var thunks = []func{}
		  func effect(compute) { thunks = append(thunks, func() { println(compute()) }) }
		  func main() { a := 3  effect(func() { return a * a })  t := thunks[0]  t()  a = 4  t() }`, "9 16"},
	}
	for i, c := range cases {
		prog := progFromSrc(t, c.src)
		out, err := RunCaptured(prog)
		if err != nil {
			t.Fatalf("case %d run: %v", i, err)
		}
		if got := strings.Join(strings.Fields(out), " "); got != c.want {
			t.Fatalf("case %d = %q, want %q", i, got, c.want)
		}
	}
}

// A closure can capture and MUTATE an aggregate local (a slice/map/struct), and the
// change persists across calls. The heap box for such a local is zeroed via calloc —
// previously codegen emitted `*box = {0}`, invalid C for a slice. This is what lets
// the reactive runtime's `each` capture its `old []int` key set.
func TestCapturedSliceMutation(t *testing.T) {
	prog := progFromSrc(t, `
func acc() (push) { xs := []int{}  push = func(v) { xs = append(xs, v)  return len(xs) } }
func main() { p := acc()  println(p(10))  println(p(20))  println(p(30)) }`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := strings.Join(strings.Fields(out), " "); got != "1 2 3" {
		t.Fatalf("captured slice mutation = %q, want %q", got, "1 2 3")
	}
}
