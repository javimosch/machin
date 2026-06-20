package main

import "testing"

// These tests cover three additional example programs shipped under
// examples/complex/: an in-place bubble sort, the digital-root reduction,
// and the recursive Josephus survivor. They exercise nested while loops with
// indexed slice assignment and self-recursion with modular arithmetic.

func TestBubbleSort(t *testing.T) {
	got := runNative(t,
		`func main() { xs := []int{5, 2, 9, 1, 7, 3} n := len(xs) i := 0 while i < n { j := 0 while j < n - 1 - i { if xs[j] > xs[j+1] { t := xs[j] xs[j] = xs[j+1] xs[j+1] = t } j = j + 1 } i = i + 1 } i = 0 while i < n { print(xs[i], "") i = i + 1 } println("") }`)
	if got != "1 2 3 5 7 9 \n" {
		t.Fatalf("got %q", got)
	}
}

func TestDigitalRoot(t *testing.T) {
	got := runNative(t,
		`func droot(n) { while n >= 10 { s := 0 while n > 0 { s = s + n % 10 n = n / 10 } n = s } return n }`,
		`func main() { println(droot(9875), droot(12345), droot(0)) }`)
	if got != "2 6 0\n" {
		t.Fatalf("got %q", got)
	}
}

func TestJosephus(t *testing.T) {
	got := runNative(t,
		`func josephus(n, k) { if n == 1 { return 0 } return (josephus(n-1, k) + k) % n }`,
		`func main() { println(josephus(7, 3), josephus(41, 3), josephus(5, 2)) }`)
	if got != "3 30 2\n" {
		t.Fatalf("got %q", got)
	}
}
