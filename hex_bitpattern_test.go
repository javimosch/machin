package main

import "testing"

// #556: a hex/binary/octal literal is a bit pattern, not a magnitude — the top
// bit being set shouldn't be a parse error. If it fits in 64 bits unsigned,
// interpret it as its two's-complement int value, matching the arithmetic
// (0 - 7046029254386353131 already produces this exact value).
func TestHexLiteralTopBitSetParses(t *testing.T) {
	src := `func main() { x := 0x9E3779B97F4A7C15
  println(str(x)) }`
	if got, want := runNative(t, src), "-7046029254386353131\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBinaryLiteralTopBitSetParses(t *testing.T) {
	// 64 ones == -1 in two's complement.
	src := `func main() { x := 0b1111111111111111111111111111111111111111111111111111111111111111
  println(str(x)) }`
	if got, want := runNative(t, src), "-1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOctalLiteralTopBitSetParses(t *testing.T) {
	// 0o1777777777777777777777 is the 64-bit all-ones bit pattern == -1.
	src := `func main() { x := 0o1777777777777777777777
  println(str(x)) }`
	if got, want := runNative(t, src), "-1\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecimalLiteralAboveInt64MaxStillErrors(t *testing.T) {
	_, err := ParseFunc(`func main() { x := 99999999999999999999
  println(str(x)) }`)
	if err == nil {
		t.Fatal("expected an error for a decimal literal above 2^63-1, got nil")
	}
}

func TestHexLiteralAbove64BitsStillErrors(t *testing.T) {
	_, err := ParseFunc(`func main() { x := 0xFFFFFFFFFFFFFFFFF
  println(str(x)) }`)
	if err == nil {
		t.Fatal("expected an error for a hex literal above 2^64-1, got nil")
	}
}
