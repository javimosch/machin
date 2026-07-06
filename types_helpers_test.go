package main

import (
	"strings"
	"testing"
)

func TestKindString(t *testing.T) {
	cases := []struct {
		k    Kind
		want string
	}{
		{KVar, "var"},
		{KNum, "num"},
		{KInt, "int"},
		{KFloat, "float"},
		{KBool, "bool"},
		{KString, "string"},
		{KVoid, "void"},
		{KSlice, "slice"},
		{KStruct, "struct"},
		{KChan, "chan"},
		{KMap, "map"},
		{KFunc, "func"},
		{KBytes, "bytes"},
		{Kind(999), "?"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("Kind(%d).String() = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestIsNumeric(t *testing.T) {
	cases := []struct {
		k    Kind
		want bool
	}{
		{KInt, true},
		{KFloat, true},
		{KNum, true},
		{KBool, false},
		{KString, false},
		{KVar, false},
	}
	for _, c := range cases {
		if got := isNumeric(c.k); got != c.want {
			t.Errorf("isNumeric(%v) = %v, want %v", c.k, got, c.want)
		}
	}
}

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

// The new*Slot constructors must set the Kind and cross-reference the fields
// specific to their kind (fsig/mkey+mval/elem/sname) on the returned slot.
func TestNewCompositeSlots(t *testing.T) {
	c := &Checker{}

	sig := &funcSig{params: []int{0, 1}, ret: 2}
	fn := newFuncSlot(c, sig)
	if c.kind[fn] != KFunc {
		t.Errorf("newFuncSlot: kind = %v, want KFunc", c.kind[fn])
	}
	if c.fsig[fn] != sig {
		t.Errorf("newFuncSlot: fsig = %v, want %v", c.fsig[fn], sig)
	}

	key := newSlot(c, KString)
	val := newSlot(c, KInt)
	m := newMapSlot(c, key, val)
	if c.kind[m] != KMap {
		t.Errorf("newMapSlot: kind = %v, want KMap", c.kind[m])
	}
	if c.mkey[m] != key || c.mval[m] != val {
		t.Errorf("newMapSlot: mkey/mval = %d/%d, want %d/%d", c.mkey[m], c.mval[m], key, val)
	}

	elem := newSlot(c, KFloat)
	sl := newSliceSlot(c, elem)
	if c.kind[sl] != KSlice {
		t.Errorf("newSliceSlot: kind = %v, want KSlice", c.kind[sl])
	}
	if c.elem[sl] != elem {
		t.Errorf("newSliceSlot: elem = %d, want %d", c.elem[sl], elem)
	}

	st := newStructSlot(c, "Point")
	if c.kind[st] != KStruct {
		t.Errorf("newStructSlot: kind = %v, want KStruct", c.kind[st])
	}
	if c.sname[st] != "Point" {
		t.Errorf("newStructSlot: sname = %q, want %q", c.sname[st], "Point")
	}

	chanElem := newSlot(c, KBool)
	ch := newChanSlot(c, chanElem)
	if c.kind[ch] != KChan {
		t.Errorf("newChanSlot: kind = %v, want KChan", c.kind[ch])
	}
	if c.elem[ch] != chanElem {
		t.Errorf("newChanSlot: elem = %d, want %d", c.elem[ch], chanElem)
	}
}
