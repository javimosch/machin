package main

import (
	"strings"
	"testing"
)

// make([]T, n) / make([]T, len, cap) — preallocation (#584).
//
// The three-argument form exists to skip append's growth entirely: an append loop
// into a slice with room to spare never reallocates at all, which is a cost #578's
// in-place growth reduces but cannot remove.

func TestMakeSliceZeroesElements(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := make([]int, 3)
    println("ints=" + str(len(xs)) + ":" + str(xs[0]) + ":" + str(xs[2]))
    ss := make([]string, 2)
    println("strs=[" + ss[0] + "][" + ss[1] + "]")
    fs := make([]float, 2)
    println("floats=" + str(fs[0]))
    bs := make([]bool, 1)
    println("bools=" + str(bs[0]))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Zeroed, not garbage — callers index straight in without writing first.
	for _, want := range []string{"ints=3:0:0", "strs=[][]", "floats=0", "bools=false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMakeSliceWithCapacityAppendsWithoutGrowing(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := make([]int, 0, 100)
    i := 0
    while i < 100 { xs = append(xs, i)  i = i + 1 }
    println("len=" + str(len(xs)) + " first=" + str(xs[0]) + " last=" + str(xs[99]))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "len=100 first=0 last=99") {
		t.Fatalf("unexpected:\n%s", out)
	}
}

// len and cap are independent: the length is what len() reports and what is
// zeroed, the capacity only reserves room.
func TestMakeSliceLenAndCapAreDistinct(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := make([]int, 3, 50)
    println("len=" + str(len(xs)))
    xs = append(xs, 9)
    println("after=" + str(len(xs)) + " idx3=" + str(xs[3]) + " idx0=" + str(xs[0]))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"len=3", "after=4 idx3=9 idx0=0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// A capacity below the length would produce a slice whose len exceeds its
// allocation — the very next index is then a heap overrun. It is raised to the
// length rather than trusted. Negative values clamp to 0 for the same reason.
func TestMakeSliceClampsNonsenseSizes(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    a := make([]int, 5, 2)
    println("cap_below_len=" + str(len(a)) + ":" + str(a[4]))
    b := make([]int, -3)
    println("negative_len=" + str(len(b)))
    c := make([]int, 0)
    println("zero=" + str(len(c)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"cap_below_len=5:0", "negative_len=0", "zero=0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// Sizes are ordinary expressions, not literals.
func TestMakeSliceSizesAreExpressions(t *testing.T) {
	prog := progFromSrc(t, `
func size() (n) { n = 4 }

func main() {
    k := 2
    xs := make([]int, k * 3, size() * 10)
    println("len=" + str(len(xs)))
    xs = append(xs, 1)
    println("after=" + str(len(xs)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"len=6", "after=7"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// A preallocated slice is the most clearly-owned way to start one, so it must
// still qualify for #578's in-place append. Before MakeSlice was taught to the
// alias analysis it was refused as "assigned a value other than append(v, …) or a
// slice literal" — exactly backwards.
func TestMakeSliceStillQualifiesForInPlaceAppend(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := make([]int, 0, 8)
    i := 0
    while i < 20 { xs = append(xs, i)  i = i + 1 }
    println(len(xs))
}`)
	liftClosures(prog)
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	var found bool
	for _, a := range analyzeSliceAliasing(prog, c) {
		if a.Var == "xs" {
			found = true
			if !a.Unaliased {
				t.Fatalf("a make()d slice must still qualify, refused: %s", a.Reason)
			}
			if a.Appends != 1 {
				t.Fatalf("expected 1 append site, got %d", a.Appends)
			}
		}
	}
	if !found {
		t.Fatal("xs missing from the alias report")
	}
}

func TestMakeSliceRejectsBadForms(t *testing.T) {
	for _, src := range []string{
		`func main() { xs := make([]int)  println(len(xs)) }`,   // no length
		`func main() { xs := make([]int, )  println(len(xs)) }`, // dangling comma
		`func main() { xs := make([], 3)  println(len(xs)) }`,   // no element type
	} {
		if _, err := progFromSrcErr(src); err == nil {
			t.Fatalf("expected a parse error for: %s", src)
		}
	}
}
