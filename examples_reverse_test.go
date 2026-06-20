package main

import "testing"

// Mirrors examples/complex/reverse_digits.mfl: reversing an integer's digits
// via a while loop and modulo, and using that to test for numeric palindromes.

func TestReverseDigits(t *testing.T) {
	got := runNative(t,
		`func reverse(n) { r := 0 while n > 0 { r = r * 10 + n % 10 n = n / 10 } return r }`,
		`func main() { println(reverse(12345), reverse(1200), reverse(7)) }`)
	if got != "54321 21 7\n" {
		t.Fatalf("got %q", got)
	}
}

func TestNumericPalindrome(t *testing.T) {
	got := runNative(t,
		`func reverse(n) { r := 0 while n > 0 { r = r * 10 + n % 10 n = n / 10 } return r }`,
		`func is_palindromic(n) { if n == reverse(n) { return 1 } return 0 }`,
		`func main() { println(is_palindromic(121), is_palindromic(123)) }`)
	if got != "1 0\n" {
		t.Fatalf("got %q", got)
	}
}
