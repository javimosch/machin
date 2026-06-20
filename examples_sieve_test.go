package main

import "testing"

// Tests for the sieve and double_factorial examples in examples/complex/.

func TestSieveExample(t *testing.T) {
	got := runNative(t,
		`func count_primes_sieve(n) { flags := []int{} i := 0 while i <= n { flags = append(flags, 1) i = i + 1 } flags[0] = 0 flags[1] = 0 p := 2 while p * p <= n { if flags[p] == 1 { m := p * p while m <= n { flags[m] = 0 m = m + p } } p = p + 1 } count := 0 i = 2 while i <= n { if flags[i] == 1 { count = count + 1 } i = i + 1 } return count }`,
		`func main() { println(count_primes_sieve(30), count_primes_sieve(100), count_primes_sieve(1000)) }`)
	if got != "10 25 168\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDoubleFactorialExample(t *testing.T) {
	got := runNative(t,
		`func double_fact(n) { r := 1 while n > 1 { r = r * n n = n - 2 } return r }`,
		`func main() { println(double_fact(0), double_fact(6), double_fact(10)) }`)
	if got != "1 48 3840\n" {
		t.Fatalf("got %q", got)
	}
}
