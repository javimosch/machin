package main

import (
	"strings"
	"testing"
)

// json() must write floats so they read back exactly (#774): %g kept 6
// significant digits, so saving and loading silently changed the value.
func TestJSONFloatRoundTrips(t *testing.T) {
	out := strings.TrimSpace(runFullProg(t, `type W struct { v float  xs []float }
func main() {
    third := 1.0
    third = third / 3.0
    sum := 0.1
    sum = sum + 0.2
    vals := []float{1234567.89, 100000005.0, third, sum, 0.1, -2.5, 0.0, 1e300, 5e-324, 9007199254740993.0}
    i := 0
    while i < len(vals) {
        j := json(vals[i])
        if parse_float(j) != vals[i] { println("LOST " + j) }
        i = i + 1
    }
    println(json(1234567.89))
    println(json(0.1))
    println(json(100000005.0))
    println(json(sum))
    w := parse(json(W{v: third, xs: []float{1234567.89, sum}}), W{})
    println(str(w.v == third && w.xs[0] == 1234567.89 && w.xs[1] == sum))
    println(str(1234567.89))
}`))
	want := "1234567.89\n0.1\n100000005\n0.30000000000000004\ntrue\n1.23457e+06"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}
