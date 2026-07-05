package main

import "testing"

// TestByteAtOutOfRange covers mfl_byte_at (codegen.go): a negative index or an
// index at/past len must yield -1 rather than reading out of bounds.
func TestByteAtOutOfRange(t *testing.T) {
	got := runNative(t, `func main(){ b := from_hex("aabb") println(byte_at(b, -1)) println(byte_at(b, 2)) println(byte_at(b, 0)) }`)
	if want := "-1\n-1\n170\n"; got != want {
		t.Fatalf("byte_at out-of-range: got %q, want %q", got, want)
	}
}

// TestBytesSubClamping covers mfl_bytes_sub (codegen.go): a negative start
// clamps to 0, an end past len clamps to len, and end < start yields an empty
// (not negative-length) slice.
func TestBytesSubClamping(t *testing.T) {
	got := runNative(t, `func main(){
		b := from_hex("aabbcc")
		println(to_hex(bytes_sub(b, -5, 2)))
		println(to_hex(bytes_sub(b, 1, 99)))
		println(len(bytes_sub(b, 2, 0)))
	}`)
	if want := "aabb\nbbcc\n0\n"; got != want {
		t.Fatalf("bytes_sub clamping: got %q, want %q", got, want)
	}
}

// TestBytesIndexEmptyNeedleAndAbsent covers mfl_bytes_index (codegen.go): an
// empty needle matches at the (clamped) from offset, and an absent needle
// yields -1.
func TestBytesIndexEmptyNeedleAndAbsent(t *testing.T) {
	got := runNative(t, `func main(){
		h := from_hex("aabbcc")
		println(bytes_index(h, bytes(""), 1))
		println(bytes_index(h, from_hex("ddee"), 0))
		println(bytes_index(h, from_hex("bb"), -3))
	}`)
	if want := "1\n-1\n1\n"; got != want {
		t.Fatalf("bytes_index edge cases: got %q, want %q", got, want)
	}
}

// TestFromHexSkipsNonHexAndDropsTrailingNibble covers mfl_bytes_unhex
// (codegen.go): separators like spaces/colons are skipped rather than
// erroring, and a dangling odd hex digit at the end is silently dropped.
func TestFromHexSkipsNonHexAndDropsTrailingNibble(t *testing.T) {
	got := runNative(t, `func main(){
		println(to_hex(from_hex("aa:bb cc")))
		println(to_hex(from_hex("aabbc")))
		println(len(from_hex("")))
	}`)
	if want := "aabbcc\naabb\n0\n"; got != want {
		t.Fatalf("from_hex edge cases: got %q, want %q", got, want)
	}
}
