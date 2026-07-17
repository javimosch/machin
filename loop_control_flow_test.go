package main

import "testing"

// runFullProg compiles a full program source (which may declare types), runs it,
// and returns stdout — used where a `type` declaration rules out runNative,
// which only parses individual functions.
func runFullProg(t *testing.T, src string) string {
	t.Helper()
	out, err := RunCaptured(progFromSrc(t, src))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

// Loop control-flow fixture sweep: pin the semantics of `continue` and `break`
// across range loops, condition loops, nested loops, and map iteration.
//
// Motivated by issue #449 ("segfault: continue in a range loop after calling a
// generic fn on a parse()'d struct"). The reporter suspected `continue` skipping
// cleanup for temporaries flowing through a generic call — and asked for a
// "fixture sweep" of loop control-flow edges. Diagnosis showed the crash is
// actually the parse()/NULL-string-field bug (tracked separately in #450):
// an absent JSON string field is left NULL, so len()/strlen(NULL) segfaults
// regardless of the loop shape. These fixtures pin that the control-flow
// machinery itself is correct — continue/break behave exactly as expected when
// the values involved are well-formed — so future regressions in loop codegen
// are caught here, and the loop mechanics stay ruled out as a suspect.

// TestContinueGenericFnStructField mirrors #449's exact shape but with a struct
// *literal* (well-formed, non-NULL fields) instead of parse(). continue must
// skip the empty-string tokens and keep only the populated one. This isolates
// the control-flow path as innocent — the crash lived in parse(), not here.
func TestContinueGenericFnStructField(t *testing.T) {
	got := runFullProg(t, `
type T2 struct { a string  b string  c string }
func getv(t, name) (v) {
	if name == "a" { v = t.a }
	if name == "b" { v = t.b }
	if name == "c" { v = t.c }
}
func main() {
	t := T2{a: "", b: "hello", c: ""}
	for _, name := range []string{"a", "b", "c"} {
		v := getv(t, name)
		if len(v) == 0 { continue }
		println(name + "=" + v)
	}
	println("done")
}`)
	const want = "b=hello\ndone\n"
	if got != want {
		t.Fatalf("got %q, want %q (continue must skip empty tokens, not crash)", got, want)
	}
}

// TestBreakRangeStopsEarly pins that break exits a range loop immediately,
// leaving later elements unvisited.
func TestBreakRangeStopsEarly(t *testing.T) {
	got := runNative(t,
		`func main() {
			sum := 0
			for _, n := range []int{1, 2, 3, 4, 5} {
				if n == 4 { break }
				sum = sum + n
			}
			println(sum)
		}`)
	if got != "6\n" { // 1 + 2 + 3, stopped before 4
		t.Fatalf("got %q, want %q", got, "6\n")
	}
}

// TestNestedRangeInnerContinue pins that a continue in the inner loop only
// skips the current inner iteration and does not disturb the outer loop.
func TestNestedRangeInnerContinue(t *testing.T) {
	got := runNative(t,
		`func main() {
			total := 0
			for _, i := range []int{1, 2, 3} {
				for _, j := range []int{1, 2, 3} {
					if j == 2 { continue }
					total = total + i*j
				}
			}
			println(total)
		}`)
	if got != "24\n" { // sum over i of (i*1 + i*3) = 4i => 4+8+12
		t.Fatalf("got %q, want %q", got, "24\n")
	}
}

// TestNestedRangeInnerBreak pins that break in the inner loop only terminates
// the inner loop; the outer loop keeps iterating.
func TestNestedRangeInnerBreak(t *testing.T) {
	got := runNative(t,
		`func main() {
			out := ""
			for _, i := range []int{1, 2, 3} {
				for _, j := range []int{1, 2, 3} {
					if j == 2 { break }
					out = out + str(i) + ":" + str(j) + " "
				}
			}
			println(out)
		}`)
	if got != "1:1 2:1 3:1 \n" {
		t.Fatalf("got %q, want %q", got, "1:1 2:1 3:1 \n")
	}
}

// TestContinueForCondAdvances pins that continue in a condition loop jumps to
// the loop condition (not into an infinite spin) — the increment happens before
// the continue, so the loop still makes progress and terminates.
func TestContinueForCondAdvances(t *testing.T) {
	got := runNative(t,
		`func main() {
			i := 0
			sum := 0
			for i < 10 {
				i = i + 1
				if i % 2 == 0 { continue }
				sum = sum + i
			}
			println(sum)
		}`)
	if got != "25\n" { // 1 + 3 + 5 + 7 + 9
		t.Fatalf("got %q, want %q", got, "25\n")
	}
}

// TestMapRangeContinue pins that continue works inside a map range loop. Map
// iteration order is unspecified, but the filtered sum is order-independent.
func TestMapRangeContinue(t *testing.T) {
	got := runNative(t,
		`func main() {
			m := make(map[string]int)
			m["a"] = 1  m["b"] = 2  m["c"] = 3  m["d"] = 4
			sum := 0
			for _, v := range m {
				if v % 2 == 0 { continue }
				sum = sum + v
			}
			println(sum)
		}`)
	if got != "4\n" { // odds only: 1 + 3
		t.Fatalf("got %q, want %q", got, "4\n")
	}
}

// TestContinueThenAppend pins that a slice built with append across a range loop
// with continue keeps exactly the un-skipped elements.
func TestContinueThenAppend(t *testing.T) {
	got := runNative(t,
		`func main() {
			out := []int{}
			for _, n := range []int{1, 2, 3, 4, 5, 6} {
				if n % 3 == 0 { continue }
				out = append(out, n)
			}
			total := 0
			for _, x := range out { total = total + x }
			println(len(out), total)
		}`)
	if got != "4 12\n" { // kept 1,2,4,5 => 4 items, sum 12
		t.Fatalf("got %q, want %q", got, "4 12\n")
	}
}

// TestBreakInfiniteFor pins that break is the way out of a bare `for {}` loop.
func TestBreakInfiniteFor(t *testing.T) {
	got := runNative(t,
		`func main() {
			i := 0
			for {
				if i >= 5 { break }
				i = i + 1
			}
			println(i)
		}`)
	if got != "5\n" {
		t.Fatalf("got %q, want %q", got, "5\n")
	}
}

// TestContinueWithIndex pins that the range index variable stays correct across
// continue — skipped iterations don't renumber the surviving ones.
func TestContinueWithIndex(t *testing.T) {
	got := runNative(t,
		`func main() {
			words := []string{"skip", "keep", "skip", "yes"}
			out := ""
			for idx, w := range words {
				if w == "skip" { continue }
				out = out + str(idx) + ":" + w + " "
			}
			println(out)
		}`)
	if got != "1:keep 3:yes \n" {
		t.Fatalf("got %q, want %q", got, "1:keep 3:yes \n")
	}
}
