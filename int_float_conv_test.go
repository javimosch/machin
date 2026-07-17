package main

import "testing"

// The int() / float() builtins are plain C casts in codegen.go
// (`((int64_t)(x))` and `((double)(x))`). Those casts carry two contracts that
// user code — especially the quantized-inference kernels that round floats into
// int8 lanes — leans on but which nothing pins directly:
//
//  1. int(double) truncates toward ZERO, not toward negative infinity. It is the
//     C cast, so it matches trunc(), NOT floor() or round(). int(-3.9) == -3,
//     whereas floor(-3.9) == -4 and round(-3.9) == -4.
//  2. int() actually changes the value's type to a machine integer, so the
//     result participates in integer division and modulo — not float division.
//
// float() is the inverse widening: an int promoted to double so that a
// subsequent `/` is real division instead of integer division.

// TestIntConvTruncatesTowardZero pins that int() rounds toward zero for both
// signs, agreeing with trunc() and disagreeing with floor()/round() on
// negatives. -0.5 must land on plain 0 (not -0), and an already-integral double
// is unchanged.
func TestIntConvTruncatesTowardZero(t *testing.T) {
	got := runNative(t, `func main() {
		println(int(3.9))
		println(int(-3.9))
		println(int(-0.5))
		println(int(2.0))
		println(float(int(-3.9)) == trunc(-3.9))
		println(floor(-3.9))
	}`)
	if want := "3\n-3\n0\n2\ntrue\n-4\n"; got != want {
		t.Fatalf("int() truncation contract: got %q, want %q", got, want)
	}
}

// TestIntConvYieldsIntegerType pins that int()'s result is a real integer: two
// int() operands divide with integer (truncating) division and support %, and
// an int() feeds ordinary integer arithmetic. If int() secretly stayed a double,
// int(7.9)/int(2.1) would be 3.5 rather than 3.
func TestIntConvYieldsIntegerType(t *testing.T) {
	got := runNative(t, `func main() {
		println(int(7.9) / int(2.1))
		println(int(7.9) % 2)
		println(int(3.7) + 1)
	}`)
	if want := "3\n1\n4\n"; got != want {
		t.Fatalf("int() integer-type contract: got %q, want %q", got, want)
	}
}

// TestFloatConvWidensForRealDivision pins float() as the inverse of int(): an
// int promoted to double so `/` is real division (7/2 -> 3.5, not 3), and the
// int->float->int round trip is exact for small magnitudes.
func TestFloatConvWidensForRealDivision(t *testing.T) {
	got := runNative(t, `func main() {
		x := 7
		println(float(x) / 2.0)
		println(int(float(5)) == 5)
		println(float(3) + float(4))
	}`)
	if want := "3.5\ntrue\n7\n"; got != want {
		t.Fatalf("float() widening contract: got %q, want %q", got, want)
	}
}
