package main

import (
	"fmt"
	"strings"
	"testing"
)

// ushr is checked against Go's own unsigned shift for every count 0..63, plus the
// out-of-range counts, with and without --safe's build. The operands come from a
// slice so the C compiler sees run-time values, not literals.
func TestUshr(t *testing.T) {
	values := []int64{0, 1, -1, 42, -42, -9223372036854775808, 9223372036854775807, 0x0123456789abcdef, -81985529216486896}
	counts := []int64{}
	for n := int64(0); n < 64; n++ {
		counts = append(counts, n)
	}
	counts = append(counts, 64, 65, 128, -1, -64, 9223372036854775807)
	var lit, nums []string
	var want strings.Builder
	for _, v := range values {
		if v == -9223372036854775808 {
			lit = append(lit, "1 << 63")
		} else {
			lit = append(lit, fmt.Sprint(v))
		}
		for _, n := range counts {
			// A negative count is a huge uint64, so Go also gives 0.
			fmt.Fprintf(&want, "%d\n", int64(uint64(v)>>uint64(n)))
		}
	}
	for _, n := range counts {
		nums = append(nums, fmt.Sprint(n))
	}
	prog := progFromSrc(t, fmt.Sprintf(`
func main() {
    vs := []int{%s}
    ns := []int{%s}
    for _, v := range vs { for _, n := range ns { println(str(ushr(v, n))) } }
}`, strings.Join(lit, ", "), strings.Join(nums, ", ")))
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != want.String() {
		got, exp := strings.Split(out, "\n"), strings.Split(want.String(), "\n")
		for i := range exp {
			if i >= len(got) || got[i] != exp[i] {
				t.Fatalf("value %d, count %s: line %d differs, want %s", values[i/len(counts)], nums[i%len(counts)], i, exp[i])
			}
		}
		t.Fatalf("extra output")
	}
}

// ushr's operands are ints: a float must not slip through.
func TestUshrRejectsFloat(t *testing.T) {
	prog := progFromSrc(t, `func main() { x := 1.5  println(str(ushr(x, 1))) }`)
	if _, err := CompileToC(prog, false); err == nil {
		t.Fatal("ushr(float, int) typechecked")
	}
}
