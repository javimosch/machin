package main

import "testing"

// These tests lock in language surface that the original suite exercises only
// incidentally: the comparison/logical operators, the modulo operator, and
// mixed-precedence arithmetic. They guard against regressions in the lexer,
// parser, and codegen for constructs that the example programs rely on.

func TestComparisonOperators(t *testing.T) {
	got := runNative(t,
		`func main() { println(1 < 2, 2 <= 2, 3 > 4, 4 >= 4, 5 == 5, 5 != 6) }`)
	if got != "true true false true true true\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLogicalOperators(t *testing.T) {
	got := runNative(t,
		`func main() { println(true && false, true || false, !false && true) }`)
	if got != "false true true\n" {
		t.Fatalf("got %q", got)
	}
}

func TestModuloAndPrecedence(t *testing.T) {
	got := runNative(t,
		`func main() { println(17 % 5, 2 + 3 * 4 - 1, (2 + 3) * 4) }`)
	if got != "2 13 20\n" {
		t.Fatalf("got %q", got)
	}
}

func TestNestedCallsAndEarlyReturn(t *testing.T) {
	got := runNative(t,
		`func sign(n) { if n < 0 { return -1 } if n > 0 { return 1 } return 0 }`,
		`func main() { println(sign(-7), sign(0), sign(42)) }`)
	if got != "-1 0 1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringConcatChain(t *testing.T) {
	got := runNative(t,
		`func main() { s := "" i := 0 for i < 3 { s = s + str(i) i = i + 1 } println(s) }`)
	if got != "012\n" {
		t.Fatalf("got %q", got)
	}
}
