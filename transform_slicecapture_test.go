package main

import (
	"strings"
	"testing"
)

// A closure whose ONLY reference to an outer variable is inside a slice-range
// bound (x[lo:], x[:hi], x[lo:hi]) must still capture that variable. The
// free-identifier walker (transform.go) previously had no *SliceExpr case, so
// such a variable was invisible to closure lifting and either failed to
// compile or read garbage instead of the captured value.
func TestClosureCapturesSliceBound(t *testing.T) {
	cases := []struct{ src, want string }{
		{`func main() {
			xs := []int{1, 2, 3, 4, 5}
			lo := 2
			f := func() { s := xs[lo:] println(len(s)) println(s[0]) }
			f()
		}`, "3 3"},
		{`func main() {
			xs := []int{1, 2, 3, 4, 5}
			hi := 3
			f := func() { s := xs[:hi] println(len(s)) println(s[2]) }
			f()
		}`, "3 3"},
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
