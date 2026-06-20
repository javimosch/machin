package main

import "testing"

// Tests for the fizzbuzz, tribonacci, and sum_factorials examples.
// These exercise else-if chains, iterative state machines, and nested
// function calls inside loops — all compiled to native and executed.

func TestFizzBuzzExample(t *testing.T) {
	got := runNative(t,
		`func main() { i := 1 while i <= 15 { if i % 15 == 0 { println("FizzBuzz") } else if i % 3 == 0 { println("Fizz") } else if i % 5 == 0 { println("Buzz") } else { println(i) } i = i + 1 } }`)
	want := "1\n2\nFizz\n4\nBuzz\nFizz\n7\n8\nFizz\nBuzz\n11\nFizz\n13\n14\nFizzBuzz\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTribonacciExample(t *testing.T) {
	got := runNative(t,
		`func trib(n) { if n < 2 { return 0 } if n == 2 { return 1 } a := 0 b := 0 c := 1 i := 3 while i <= n { t := a + b + c a = b b = c c = t i = i + 1 } return c }`,
		`func main() { i := 0 while i <= 12 { print(trib(i), "") i = i + 1 } println("") }`)
	if got != "0 0 1 1 2 4 7 13 24 44 81 149 274 \n" {
		t.Fatalf("got %q", got)
	}
}

func TestSumFactorialsExample(t *testing.T) {
	got := runNative(t,
		`func fact(n) { r := 1 i := 2 while i <= n { r = r * i i = i + 1 } return r }`,
		`func main() { sum := 0 i := 1 while i <= 6 { sum = sum + fact(i) i = i + 1 } println(sum) }`)
	if got != "873\n" {
		t.Fatalf("got %q", got)
	}
}
