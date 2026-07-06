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

func TestParseCallbackType(t *testing.T) {
	cases := []struct {
		enc        string
		wantParams []string
		wantRet    string
	}{
		{"cb()", nil, ""},
		{"cb()int", nil, "int"},
		{"cb(int)", []string{"int"}, ""},
		{"cb(int,f32)i8", []string{"int", "f32"}, "i8"},
		{"cb(ptr,string)float", []string{"ptr", "string"}, "float"},
	}
	for _, c := range cases {
		params, ret := parseCallbackType(c.enc)
		if !reflect.DeepEqual(params, c.wantParams) || ret != c.wantRet {
			t.Errorf("parseCallbackType(%q) = (%v, %q), want (%v, %q)",
				c.enc, params, ret, c.wantParams, c.wantRet)
		}
	}
}
