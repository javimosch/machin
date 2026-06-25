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
