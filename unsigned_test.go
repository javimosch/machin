package main

import (
	"fmt"
	"math/bits"
	"strings"
	"testing"
)

// ult/udiv/urem/umulh against Go's unsigned arithmetic (#758), including the
// values whose partial products carry.
func TestUnsignedBuiltins(t *testing.T) {
	values := []uint64{0, 1, 2, 3, 0xffffffff, 0x100000000, 0x1ffffffff, 0x7fffffffffffffff, 0x8000000000000000, 0xffffffff00000000, 0xfffffffffffffffe, 0xffffffffffffffff, 0x0123456789abcdef}
	var lit []string
	var want strings.Builder
	for _, a := range values {
		if a == 0x8000000000000000 {
			lit = append(lit, "1 << 63")
		} else {
			lit = append(lit, fmt.Sprint(int64(a)))
		}
		for _, b := range values {
			hi, _ := bits.Mul64(a, b)
			q, r := "-", "-"
			if b != 0 {
				q, r = fmt.Sprint(int64(a/b)), fmt.Sprint(int64(a%b))
			}
			fmt.Fprintf(&want, "%v %s %s %d\n", a < b, q, r, int64(hi))
		}
	}
	src := fmt.Sprintf(`
func main() {
    vs := []int{%s}
    for _, a := range vs { for _, b := range vs {
        q := "-"
        r := "-"
        if b != 0 { q = str(udiv(a, b))  r = str(urem(a, b)) }
        println(str(ult(a, b)) + " " + q + " " + r + " " + str(umulh(a, b)))
    } }
}`, strings.Join(lit, ", "))
	out, err := RunCaptured(progFromSrc(t, src))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != want.String() {
		got, exp := strings.Split(out, "\n"), strings.Split(want.String(), "\n")
		for i := range exp {
			if i >= len(got) || got[i] != exp[i] {
				t.Fatalf("line %d: want %q, got %q", i, exp[i], got[min(i, len(got)-1)])
			}
		}
	}
}
