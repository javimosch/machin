package main

import (
	"fmt"
	"strings"
	"testing"
)

// << and >> are defined for every count (#754). The operands come from slices so
// the C compiler sees run-time values; Go's own shifts are the oracle, with a
// negative count behaving like any other count past 63.
func TestShiftCountsOutOfRange(t *testing.T) {
	values := []int64{0, 1, -1, 256, -256, 9223372036854775807}
	counts := []int64{0, 1, 63, 64, 65, 128, -1, -64}
	var lit, nums []string
	var want strings.Builder
	for _, v := range values {
		lit = append(lit, fmt.Sprint(v))
		for _, n := range counts {
			fmt.Fprintf(&want, "%d %d\n", v<<uint64(n), v>>uint64(n))
		}
	}
	for _, n := range counts {
		nums = append(nums, fmt.Sprint(n))
	}
	prog := progFromSrc(t, fmt.Sprintf(`
func main() {
    vs := []int{%s}
    ns := []int{%s}
    for _, v := range vs { for _, n := range ns { println(str(v << n) + " " + str(v >> n)) } }
}`, strings.Join(lit, ", "), strings.Join(nums, ", ")))
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != want.String() {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want.String())
	}
}
