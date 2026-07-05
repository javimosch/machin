package main

import (
	"reflect"
	"testing"
)

// isCallbackType/parseCallbackType were only exercised indirectly through
// callbackCType; these lock in the "cb(t1,t2)ret" encoding directly.
func TestIsCallbackType(t *testing.T) {
	if !isCallbackType("cb(int)") {
		t.Error(`isCallbackType("cb(int)") = false, want true`)
	}
	if !isCallbackType("cb()") {
		t.Error(`isCallbackType("cb()") = false, want true`)
	}
	if isCallbackType("int") {
		t.Error(`isCallbackType("int") = true, want false`)
	}
	if isCallbackType("") {
		t.Error(`isCallbackType("") = true, want false`)
	}
}

// TestIsCallbackTypeEdgeCases covers edge cases: strings that start with "cb("
// pass validation even if malformed (no closing paren), while those that don't
// start with "cb(" are rejected, and wrong prefix strings are rejected.
func TestIsCallbackTypeEdgeCases(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"cb", false},           // missing opening paren
		{"cb(", true},           // just the prefix (no closing paren needed)
		{"cb(int", true},        // unclosed, but starts with "cb("
		{"cb)", false},          // closing paren without opening
		{"ycb(int)", false},     // wrong prefix
		{"xcb(int)", false},     // wrong prefix
		{"cb(int)string", true}, // valid with return type
		{"cb(map,chan)", true},  // valid with multiple types
	}
	for _, c := range cases {
		if got := isCallbackType(c.input); got != c.want {
			t.Errorf("isCallbackType(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestParseCallbackType(t *testing.T) {
	cases := []struct {
		enc        string
		wantParams []string
		wantRet    string
	}{
		{"cb()", nil, ""},
		{"cb()int", nil, "int"},
		{"cb(int)", []string{"int"}, ""},
		{"cb(int)i8", []string{"int"}, "i8"},
		{"cb(int,f32)i8", []string{"int", "f32"}, "i8"},
	}
	for _, c := range cases {
		params, ret := parseCallbackType(c.enc)
		if !reflect.DeepEqual(params, c.wantParams) || ret != c.wantRet {
			t.Errorf("parseCallbackType(%q) = (%v, %q), want (%v, %q)",
				c.enc, params, ret, c.wantParams, c.wantRet)
		}
	}
}
