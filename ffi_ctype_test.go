package main

import "testing"

// ffiCType/callbackCType/cType were only exercised indirectly through full
// FFI build tests; these lock in the scalar-to-C-type mapping tables directly.
func TestFFICTypeMapping(t *testing.T) {
	cases := []struct{ mfl, want string }{
		{"ptr", "void*"},
		{"int", "int64_t"},
		{"i64", "int64_t"},
		{"i32", "int32_t"},
		{"i16", "int16_t"},
		{"i8", "int8_t"},
		{"u64", "uint64_t"},
		{"u32", "uint32_t"},
		{"u16", "uint16_t"},
		{"u8", "uint8_t"},
		{"float", "double"},
		{"f64", "double"},
		{"f32", "float"},
		{"bool", "int"},
		{"string", "const char*"},
		{"", "void"},
		{"NotAScalar", "void"},
	}
	for _, c := range cases {
		if got := ffiCType(c.mfl); got != c.want {
			t.Errorf("ffiCType(%q) = %q, want %q", c.mfl, got, c.want)
		}
	}
}

func TestIsFFIScalarAndNumeric(t *testing.T) {
	scalars := []string{"int", "float", "bool", "string", "ptr", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}
	for _, s := range scalars {
		if !isFFIScalar(s) {
			t.Errorf("isFFIScalar(%q) = false, want true", s)
		}
	}
	if isFFIScalar("Point") {
		t.Error("isFFIScalar(\"Point\") = true, want false for a cstruct name")
	}

	if isFFINumeric("string") {
		t.Error("isFFINumeric(\"string\") = true, want false")
	}
	if isFFINumeric("ptr") {
		t.Error("isFFINumeric(\"ptr\") = true, want false")
	}
	if !isFFINumeric("i32") {
		t.Error("isFFINumeric(\"i32\") = false, want true")
	}
}

func TestCallbackCType(t *testing.T) {
	cases := []struct{ enc, want string }{
		{"cb()", "void (*)(void)"},
		{"cb()int", "int64_t (*)(void)"},
		{"cb(int)", "void (*)(int64_t)"},
		{"cb(int,f32)i8", "int8_t (*)(int64_t, float)"},
	}
	for _, c := range cases {
		if got := callbackCType(c.enc); got != c.want {
			t.Errorf("callbackCType(%q) = %q, want %q", c.enc, got, c.want)
		}
	}
}

func TestExternCTypeRouting(t *testing.T) {
	if got := externCType(""); got != "void" {
		t.Errorf("externCType(\"\") = %q, want void", got)
	}
	if got := externCType("cb(int)"); got != "void (*)(int64_t)" {
		t.Errorf("externCType(callback) = %q", got)
	}
	if got := externCType("i32"); got != "int32_t" {
		t.Errorf("externCType(scalar) = %q", got)
	}
	if got := externCType("Point"); got != "Point" {
		t.Errorf("externCType(cstruct) = %q, want the struct's own C name", got)
	}
}

func TestFFIMFLTypeMapping(t *testing.T) {
	cases := []struct{ ffi, want string }{
		{"ptr", "int"},
		{"f32", "float"},
		{"f64", "float"},
		{"float", "float"},
		{"bool", "bool"},
		{"string", "string"},
		{"i32", "int"},
	}
	for _, c := range cases {
		if got := ffiMFLType(c.ffi); got != c.want {
			t.Errorf("ffiMFLType(%q) = %q, want %q", c.ffi, got, c.want)
		}
	}
}

func TestCTypeAndCZeroByKind(t *testing.T) {
	cases := []struct {
		k        Kind
		wantType string
		wantZero string
	}{
		{KInt, "int64_t", "0"},
		{KFloat, "double", "0.0"},
		{KBool, "int", "0"},
		{KString, "char*", "\"\""},
		{KBytes, "mfl_bytes", "{0}"},
		{KVoid, "void", "0"},
		{KSlice, "mfl_slice", "{0}"},
		{KChan, "mfl_chan*", "0"},
		{KMap, "mfl_map*", "0"},
		{KFunc, "mfl_closure", "{0}"},
	}
	for _, c := range cases {
		if got := cType(c.k); got != c.wantType {
			t.Errorf("cType(%v) = %q, want %q", c.k, got, c.wantType)
		}
		if got := cZero(c.k); got != c.wantZero {
			t.Errorf("cZero(%v) = %q, want %q", c.k, got, c.wantZero)
		}
	}
}

// parseCallbackType is already covered by TestParseCallbackType in
// callbacktype_test.go (which this branch's history shares with main) --
// this file's own copy was a duplicate declaration from a stacked commit.
