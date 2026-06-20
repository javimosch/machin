package main

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// runExampleFile loads a real .mfl example (base64, one function per line),
// decodes and parses each function, compiles to native via cc, runs it, and
// returns stdout. This exercises the same machine-first path that `machin run`
// uses for on-disk programs.
func runExampleFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var fns []*FuncDecl
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			t.Fatalf("b64 decode in %s: %v", path, err)
		}
		fn, err := ParseFunc(string(raw))
		if err != nil {
			t.Fatalf("parse in %s: %v", path, err)
		}
		fns = append(fns, fn)
	}
	out, err := RunCaptured(fns)
	if err != nil {
		t.Fatalf("run %s: %v", path, err)
	}
	return out
}

func TestTotientExample(t *testing.T) {
	got := runExampleFile(t, "examples/complex/totient.mfl")
	// totient(1..12): 1,1,2,2,4,2,6,4,6,4,10,4
	for _, want := range []string{"7 -> 6", "12 -> 4", "1 -> 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("totient output missing %q; got:\n%s", want, got)
		}
	}
}

func TestModPowExample(t *testing.T) {
	got := runExampleFile(t, "examples/complex/mod_pow.mfl")
	for _, want := range []string{"3^13 mod 7 = 3", "2^10 mod 1000 = 24", "7^256 mod 13 = 9"} {
		if !strings.Contains(got, want) {
			t.Fatalf("mod_pow output missing %q; got:\n%s", want, got)
		}
	}
}

func TestNumDivisorsExample(t *testing.T) {
	got := runExampleFile(t, "examples/complex/num_divisors.mfl")
	// perfect square 9 has an odd divisor count (3); 12 has 6.
	for _, want := range []string{"9 has 3 divisors", "12 has 6 divisors", "1 has 1 divisors"} {
		if !strings.Contains(got, want) {
			t.Fatalf("num_divisors output missing %q; got:\n%s", want, got)
		}
	}
}

func TestCountPrimesExample(t *testing.T) {
	got := runExampleFile(t, "examples/complex/count_primes.mfl")
	// pi(10)=4, pi(100)=25, pi(1000)=168
	for _, want := range []string{"primes <= 10: 4", "primes <= 100: 25", "primes <= 1000: 168"} {
		if !strings.Contains(got, want) {
			t.Fatalf("count_primes output missing %q; got:\n%s", want, got)
		}
	}
}

func TestDigitCountExample(t *testing.T) {
	got := runExampleFile(t, "examples/complex/digit_count.mfl")
	// 0->1, 7->1, 42->2, 100000->6, -12345->5
	if got != "1\n1\n2\n6\n5\n" {
		t.Fatalf("digit_count output = %q", got)
	}
}
