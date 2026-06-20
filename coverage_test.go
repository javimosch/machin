package main

import "testing"

// These tests broaden behavioral coverage beyond mfl_test.go. They use the
// same runNative helper (defined there) and intentionally avoid duplicating any
// existing test name. They exercise loop-form variants, in-place slice
// mutation, multi-statement string building, and operator precedence.

func TestForConditionLoop(t *testing.T) {
	// `for cond {}` should behave like a while loop.
	got := runNative(t,
		`func main() { i := 0 sum := 0 for i < 5 { sum = sum + i i = i + 1 } println(sum) }`)
	if got != "10\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWhileEqualsForCondition(t *testing.T) {
	// `while cond` and `for cond` must produce identical results.
	w := runNative(t, `func main() { n := 1 acc := 1 while n <= 5 { acc = acc * n n = n + 1 } println(acc) }`)
	f := runNative(t, `func main() { n := 1 acc := 1 for n <= 5 { acc = acc * n n = n + 1 } println(acc) }`)
	if w != f || w != "120\n" {
		t.Fatalf("while=%q for=%q", w, f)
	}
}

func TestOperatorPrecedence(t *testing.T) {
	// Multiplication/division bind tighter than +/-, and % binds like * and /.
	got := runNative(t, `func main() { println(2 + 3 * 4 - 10 / 2, 17 % 5 + 1) }`)
	if got != "9 3\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSliceAppendGrowthAndIndex(t *testing.T) {
	// Repeated append must grow the slice; indexing reads back the values.
	got := runNative(t,
		`func main() { xs := []int{} i := 1 for i <= 5 { xs = append(xs, i * i) i = i + 1 } println(len(xs), xs[0], xs[4]) }`)
	if got != "5 1 25\n" {
		t.Fatalf("got %q", got)
	}
}

func TestInPlaceSliceSwap(t *testing.T) {
	// Mirrors the bubble_sort example: swapping elements mutates in place.
	got := runNative(t,
		`func swap0(xs) { t := xs[0] xs[0] = xs[1] xs[1] = t return xs }`,
		`func main() { a := []int{7, 3} swap0(a) println(a[0], a[1]) }`)
	if got != "3 7\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiStatementStringBuilding(t *testing.T) {
	// Concatenation across statements plus mixed print/println output.
	got := runNative(t,
		`func main() { s := "a" s = s + "b" s = s + "c" print(s, "") println("d" + s) }`)
	if got != "abc dabc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDigitSumHelper(t *testing.T) {
	// Mirrors the digital_root example's core arithmetic.
	got := runNative(t,
		`func sum_digits(n) { s := 0 while n > 0 { s = s + n % 10 n = n / 10 } return s }`,
		`func main() { println(sum_digits(12345)) }`)
	if got != "15\n" {
		t.Fatalf("got %q", got)
	}
}
