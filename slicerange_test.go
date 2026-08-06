package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// s[lo:hi] / s[lo:] / s[:hi] — see guide.go's []T entry for the semantics
// this pins: always a fresh copy, bounds checked like s[i].

func TestSliceRangeLengthsAndElements(t *testing.T) {
	got := runNative(t, `func main() {
    s := []int{1, 2, 3, 4}
    a := s[1:]
    b := s[:2]
    c := s[1:3]
    println(str(len(a)) + " " + str(a[0]) + " " + str(a[1]) + " " + str(a[2]))
    println(str(len(b)) + " " + str(b[0]) + " " + str(b[1]))
    println(str(len(c)) + " " + str(c[0]) + " " + str(c[1]))
}`)
	want := "3 2 3 4\n2 1 2\n2 2 3\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSliceRangeStrings(t *testing.T) {
	got := runNative(t, `func main() {
    s := []string{"a", "b", "c", "d"}
    t := s[1:3]
    println(str(len(t)) + " " + t[0] + " " + t[1])
}`)
	if got != "2 b c\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSliceRangeStructs(t *testing.T) {
	prog := progFromSrc(t, `
type P struct { x int }
func main() {
    s := []P{}
    s = append(s, P{x: 1})
    s = append(s, P{x: 2})
    s = append(s, P{x: 3})
    t := s[1:]
    println(str(len(t)) + " " + str(t[0].x) + " " + str(t[1].x))
}`)
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "2 2 3\n" {
		t.Fatalf("got %q", out)
	}
}

func TestSliceRangeIsACopyNotAView(t *testing.T) {
	got := runNative(t, `func main() {
    s := []int{1, 2, 3, 4}
    t := s[1:]
    t[0] = 99
    println(str(s[1]) + " " + str(t[0]))
}`)
	if got != "2 99\n" {
		t.Fatalf("expected mutating the slice-range result to leave the source untouched, got %q", got)
	}
}

func TestSliceRangeLoGreaterThanHiPanics(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"lo_gt_hi", `func main() { xs := []int{1, 2, 3} println(str(len(xs[2:1]))) }`, "slice bounds out of range"},
		{"hi_past_len", `func main() { xs := []int{1, 2, 3} println(str(len(xs[0:5]))) }`, "slice bounds out of range"},
		{"lo_negative", `func main() { xs := []int{1, 2, 3} lo := -1 println(str(len(xs[lo:2]))) }`, "slice bounds out of range"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bin, err := os.CreateTemp("", "mfl-slicerange-*")
			if err != nil {
				t.Fatal(err)
			}
			bin.Close()
			defer os.Remove(bin.Name())
			if err := BuildBinary(&Program{Funcs: parseFuncs(t, c.src)}, bin.Name(), true); err != nil {
				t.Fatalf("build: %v", err)
			}
			out, err := exec.Command(bin.Name()).CombinedOutput()
			if err == nil {
				t.Fatalf("expected a non-zero exit (a panic), got output %q", out)
			}
			if !strings.Contains(string(out), c.want) {
				t.Fatalf("got %q, want substring %q", out, c.want)
			}
		})
	}
}
