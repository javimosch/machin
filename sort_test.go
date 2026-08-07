package main

import (
	"os"
	"strings"
	"testing"
)

// sort / sort_by (#580). Before these existed, every program needing ordered
// output hand-rolled a comparison loop — which cost tokens, and which an agent
// under time pressure writes as an O(n^2) selection sort.

func TestSortOrderedElementTypes(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    println("ints=" + json(sort([]int{5, 3, 9, 1, 3})))
    println("strs=" + json(sort([]string{"pear", "apple", "fig"})))
    println("floats=" + json(sort([]float{2.5, 1.5, 9.0})))
    println("empty=" + json(sort([]int{})))
    println("one=" + json(sort([]int{7})))
    println("negatives=" + json(sort([]int{0, -5, 3, -1})))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"ints=[1,3,3,5,9]",
		`strs=["apple","fig","pear"]`,
		"floats=[1.5,2.5,9]",
		"empty=[]",
		"one=[7]",
		"negatives=[-5,-1,0,3]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// sort() returns a NEW slice. MFL slices share backing storage, so sorting in
// place would silently reorder every alias — the same hazard that made in-place
// realloc unsound in #578. The input must be observably untouched.
func TestSortDoesNotMutateInputOrAliases(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := []int{5, 3, 9, 1}
    alias := xs
    sorted := sort(xs)
    println("sorted=" + json(sorted))
    println("orig=" + json(xs))
    println("alias=" + json(alias))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"sorted=[1,3,5,9]",
		"orig=[5,3,9,1]",
		"alias=[5,3,9,1]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestSortByComparator(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := []int{5, 3, 9, 1}
    println("desc=" + json(sort_by(xs, func(a, b) { return a > b })))
    println("asc=" + json(sort_by(xs, func(a, b) { return a < b })))
    ss := []string{"ccc", "a", "bb"}
    println("bylen=" + json(sort_by(ss, func(a, b) { return len(a) < len(b) })))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{
		"desc=[9,5,3,1]",
		"asc=[1,3,5,9]",
		`bylen=["a","bb","ccc"]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The comparator asks "must b precede a?" exactly once per comparison, which is
// what makes the merge stable: when neither element is less than the other the
// predicate is false and the earlier element is taken first. Equal keys must
// therefore come out in input order.
func TestSortByIsStable(t *testing.T) {
	prog := progFromSrc(t, `
type P struct { name string  age int }

func main() {
    ps := []P{}
    ps = append(ps, P{name: "zoe", age: 30})
    ps = append(ps, P{name: "amy", age: 25})
    ps = append(ps, P{name: "bob", age: 30})
    ps = append(ps, P{name: "cid", age: 25})
    out := ""
    for _, p := range sort_by(ps, func(a, b) { return a.age < b.age }) {
        out = out + p.name + " "
    }
    println("order=" + out)
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// amy/cid are both 25 and amy came first; zoe/bob are both 30 and zoe came first.
	if !strings.Contains(out, "order=amy cid zoe bob") {
		t.Fatalf("unstable sort, got:\n%s", out)
	}
}

// A closure that captures state must work as a comparator — the environment
// travels through mfl_sort_by's closure pointer and back out in the generated bridge.
func TestSortByCapturingClosure(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    rank := make(map[string]int)
    rank["low"] = 1
    rank["mid"] = 2
    rank["high"] = 3
    xs := []string{"high", "low", "mid"}
    println("byrank=" + json(sort_by(xs, func(a, b) { return rank[a] < rank[b] })))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, `byrank=["low","mid","high"]`) {
		t.Fatalf("captured comparator failed:\n%s", out)
	}
}

// sort() on an element type with no natural ordering must be refused at compile
// time, and the message must point at the fix rather than just saying "no".
func TestSortRejectsUnorderedElementType(t *testing.T) {
	prog := progFromSrc(t, `
type P struct { name string  age int }

func main() {
    ps := []P{}
    ps = append(ps, P{name: "a", age: 1})
    println(len(sort(ps)))
}`)
	bin, ferr := os.CreateTemp("", "mfl-sort-*")
	if ferr != nil {
		t.Fatal(ferr)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	err := BuildBinary(prog, bin.Name(), false)
	if err == nil {
		t.Fatal("expected sort() on a struct slice to be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no ordering") || !strings.Contains(msg, "sort_by") {
		t.Fatalf("error should name the fix (sort_by), got: %s", msg)
	}
}

// Sorting a large slice exercises the merge's width doubling past the first pass
// and confirms the result is fully ordered, not just locally.
func TestSortLargeSliceFullyOrdered(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := []int{}
    i := 0
    while i < 500 {
        xs = append(xs, (i * 7919) % 1001)
        i = i + 1
    }
    s := sort(xs)
    bad := 0
    j := 1
    while j < len(s) {
        if s[j] < s[j-1] { bad = bad + 1 }
        j = j + 1
    }
    println("n=" + str(len(s)))
    println("out_of_order=" + str(bad))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"n=500", "out_of_order=0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
