package main

import (
	"strings"
	"testing"
)

func TestSplitMapType(t *testing.T) {
	cases := []struct {
		in      string
		key     string
		val     string
		wantErr bool
	}{
		{"map[int]string", "int", "string", false},
		{"map[string]map[int]bool", "string", "map[int]bool", false},
		{"map[map[int]string]bool", "map[int]string", "bool", false},
		{"map[int", "", "", true},
	}
	for _, c := range cases {
		kt, vt, err := splitMapType(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("splitMapType(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitMapType(%q): unexpected error: %v", c.in, err)
			continue
		}
		if kt != c.key || vt != c.val {
			t.Errorf("splitMapType(%q) = (%q, %q), want (%q, %q)", c.in, kt, vt, c.key, c.val)
		}
	}
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		a, b    Kind
		want    Kind
		wantErr bool
	}{
		{KInt, KInt, KInt, false},
		{KVar, KString, KString, false},
		{KFloat, KVar, KFloat, false},
		{KNum, KInt, KInt, false},
		{KFloat, KNum, KFloat, false},
		{KSlice, KSlice, KSlice, false},
		{KStruct, KStruct, KStruct, false},
		{KChan, KChan, KChan, false},
		{KMap, KMap, KMap, false},
		{KFunc, KFunc, KFunc, false},
		{KInt, KString, KVar, true},
	}
	for _, c := range cases {
		got, err := reconcile(c.a, c.b)
		if c.wantErr {
			if err == nil {
				t.Errorf("reconcile(%v, %v): expected error, got nil", c.a, c.b)
			} else if !strings.Contains(err.Error(), "type mismatch") {
				t.Errorf("reconcile(%v, %v): unexpected error message: %v", c.a, c.b, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("reconcile(%v, %v): unexpected error: %v", c.a, c.b, err)
			continue
		}
		if got != c.want {
			t.Errorf("reconcile(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
