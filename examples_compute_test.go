package main

import "testing"

// Tests for the palindrome, sum_of_squares, and binary_search examples in
// examples/complex/. Each mirrors the logic of the corresponding .mfl program
// so a regression in codegen surfaces here, not just at `./examples/run.sh`.

func TestPalindromeExample(t *testing.T) {
	got := runNative(t,
		`func reverse(n) { r := 0 while n > 0 { r = r * 10 + n % 10 n = n / 10 } return r }`,
		`func is_palindrome(n) { return reverse(n) == n }`,
		`func main() { println(is_palindrome(101), is_palindrome(123), reverse(120)) }`)
	if got != "true false 21\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSumOfSquaresExample(t *testing.T) {
	got := runNative(t,
		`func sum_of_squares(n) { s := 0 i := 1 while i <= n { s = s + i * i i = i + 1 } return s }`,
		`func main() { println(sum_of_squares(3), sum_of_squares(10)) }`)
	if got != "14 385\n" {
		t.Fatalf("got %q", got)
	}
}

func TestBinarySearchExample(t *testing.T) {
	got := runNative(t,
		`func bsearch(xs, target) { lo := 0 hi := len(xs) - 1 while lo <= hi { mid := (lo + hi) / 2 if xs[mid] == target { return mid } if xs[mid] < target { lo = mid + 1 } else { hi = mid - 1 } } return 0 - 1 }`,
		`func main() { xs := []int{1, 3, 5, 7, 9, 11, 13, 15} println(bsearch(xs, 7), bsearch(xs, 1), bsearch(xs, 15), bsearch(xs, 8)) }`)
	if got != "3 0 7 -1\n" {
		t.Fatalf("got %q", got)
	}
}
