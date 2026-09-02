package main

import (
	"strings"
	"testing"
)

// copy(v) — a value sharing no backing storage with v (#639).
//
// MFL slices are a pointer, a length and a capacity, so `b := a` copies the
// header and shares the elements. That is Go's semantics and the right default,
// but there was no way to opt out of it: a deep copy had to be written by hand,
// one line per slice field, and went stale the moment a field was added. It
// caused three shipped bugs across two game repos, and none of the symptoms
// pointed at the cause — bodies rising into the sky, rematches that were
// instant wins, an enemy spawning on the floor.

func TestCopySliceIsIndependent(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := []int{1, 2, 3}
    ys := copy(xs)
    ys[0] = 99
    println("xs=" + json(xs))
    println("ys=" + json(ys))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"xs=[1,2,3]", "ys=[99,2,3]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The motivating case: a struct whose fields are slices. `b := a` shares them;
// copy(a) does not.
func TestCopyStructWithSliceFields(t *testing.T) {
	prog := progFromSrc(t, `
type Skel struct { x []float  y []float  n int }
func main() {
    a := Skel{}
    a.x = []float{1.0}
    a.y = []float{2.0}
    a.n = 5
    shared := a
    deep := copy(a)
    shared.x[0] = 7.0
    deep.y[0] = 8.0
    deep.n = 9
    println("a.x=" + json(a.x))
    println("a.y=" + json(a.y))
    println("a.n=" + str(a.n) + " deep.n=" + str(deep.n))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// plain assignment shared x, so writing through it is visible in a
	if !strings.Contains(out, "a.x=[7]") {
		t.Fatalf("plain assignment should still alias:\n%s", out)
	}
	// the copy did not share y
	if !strings.Contains(out, "a.y=[2]") {
		t.Fatalf("copy() did not detach the slice field:\n%s", out)
	}
	if !strings.Contains(out, "a.n=5 deep.n=9") {
		t.Fatalf("scalar field not independent:\n%s", out)
	}
}

// A slice of structs that themselves contain nothing shared still has to be
// rebuilt, or writing through the copy's elements reaches the original.
func TestCopySliceOfStructs(t *testing.T) {
	prog := progFromSrc(t, `
type Brick struct { hp float }
func main() {
    xs := []Brick{}
    xs = append(xs, Brick{10.0})
    ys := copy(xs)
    ys[0].hp = 99.0
    println("xs=" + str(xs[0].hp) + " ys=" + str(ys[0].hp))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "xs=10 ys=99") {
		t.Fatalf("elements were shared:\n%s", out)
	}
}

// Nesting: the copier recurses into element and field types, so a slice inside
// a struct inside a slice is detached all the way down.
func TestCopyNestedDeeply(t *testing.T) {
	prog := progFromSrc(t, `
type Inner struct { vals []int }
type Outer struct { items []Inner }
func main() {
    in := Inner{}
    in.vals = []int{1}
    o := Outer{}
    o.items = []Inner{}
    o.items = append(o.items, in)
    c := copy(o)
    c.items[0].vals[0] = 42
    println("orig=" + str(o.items[0].vals[0]) + " copy=" + str(c.items[0].vals[0]))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "orig=1 copy=42") {
		t.Fatalf("nested slice was shared:\n%s", out)
	}
}

// A type that reaches itself. The copier for []Node references the copier for
// Node, which is emitted after it — so the emitted C needs a prototype, and
// the memo has to reserve the name before recursing or this expands forever.
func TestCopyRecursiveType(t *testing.T) {
	prog := progFromSrc(t, `
type Node struct { tag string  kids []Node }
func main() {
    kid := Node{}
    kid.tag = "leaf"
    kid.kids = []Node{}
    n := Node{}
    n.tag = "root"
    n.kids = []Node{}
    n.kids = append(n.kids, kid)
    c := copy(n)
    c.kids[0].tag = "changed"
    println("orig=" + n.kids[0].tag + " copy=" + c.kids[0].tag)
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "orig=leaf copy=changed") {
		t.Fatalf("recursive type not copied:\n%s", out)
	}
}

func TestCopyMap(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    m := make(map[string]int)
    m["a"] = 7
    c := copy(m)
    c["a"] = 8
    c["b"] = 1
    println("m[a]=" + str(m["a"]) + " c[a]=" + str(c["a"]))
    println("m len=" + str(len(m)) + " c len=" + str(len(c)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"m[a]=7 c[a]=8", "m len=1 c len=2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// The copy's capacity is exactly its length: carrying the original's spare
// capacity over would hand the copy a buffer that append() could grow into
// while the original still points at it.
func TestCopyDoesNotInheritCapacity(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    xs := []int{}
    xs = append(xs, 1)
    xs = append(xs, 2)
    ys := copy(xs)
    ys = append(ys, 3)
    ys[0] = 50
    println("xs=" + json(xs) + " ys=" + json(ys))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "xs=[1,2] ys=[50,2,3]") {
		t.Fatalf("append through the copy reached the original:\n%s", out)
	}
}

// Scalars and strings are values already; copy is the identity on them rather
// than an error, so generic code can call it without asking what it has.
func TestCopyScalarsAndStrings(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    n := copy(41) + 1
    s := copy("hi")
    b := copy(true)
    f := copy(1.5)
    println(str(n) + " " + s + " " + str(b) + " " + str(f))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "42 hi true 1.5") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestCopyEmptySlice(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    e := []int{}
    c := copy(e)
    c = append(c, 1)
    println("e=" + str(len(e)) + " c=" + str(len(c)))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "e=0 c=1") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// Refusals, each with a message that says why rather than silently handing back
// an alias. A channel and a closure are the two values for which "a copy" has
// no honest meaning.
func TestCopyRefusesChannel(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    c := make(chan int)
    d := copy(c)
    d <- 1
}`)
	_, err := RunCaptured(prog)
	if err == nil || !strings.Contains(err.Error(), "rendezvous") {
		t.Fatalf("expected a channel refusal, got %v", err)
	}
}

func TestCopyRefusesClosure(t *testing.T) {
	prog := progFromSrc(t, `
func main() {
    f := func(x){ return x + 1 }
    g := copy(f)
    println(str(g(1)))
}`)
	_, err := RunCaptured(prog)
	if err == nil || !strings.Contains(err.Error(), "captures its environment") {
		t.Fatalf("expected a closure refusal, got %v", err)
	}
}

func TestCopyArityChecked(t *testing.T) {
	prog, err := ParseProgram([]string{`func main() { xs := []int{1} ys := copy(xs, 2) println(str(len(ys))) }`})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "copy: 1 arg") {
		t.Fatalf("expected an arity error, got %v", err)
	}
}
