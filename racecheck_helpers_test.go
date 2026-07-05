package main

import "testing"

func TestClonePath(t *testing.T) {
	if got := clonePath(nil); got != nil {
		t.Fatalf("clonePath(nil) = %v, want nil", got)
	}
	if got := clonePath(accPath{}); got != nil {
		t.Fatalf("clonePath(empty) = %v, want nil", got)
	}

	orig := accPath{{field: "a"}, {field: ""}, {field: "b"}}
	clone := clonePath(orig)
	if !pathEq(orig, clone) {
		t.Fatalf("clonePath(%v) = %v, want equal path", orig, clone)
	}

	// Mutating the clone must not affect the original — it's a deep copy of the slice.
	clone[0].field = "z"
	if orig[0].field != "a" {
		t.Fatalf("mutating clone affected original: %v", orig)
	}
}

func TestPathEq(t *testing.T) {
	tests := []struct {
		name string
		a, b accPath
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, accPath{}, true},
		{"equal single field", accPath{{field: "x"}}, accPath{{field: "x"}}, true},
		{"equal index step", accPath{{field: ""}}, accPath{{field: ""}}, true},
		{"different field name", accPath{{field: "x"}}, accPath{{field: "y"}}, false},
		{"different length", accPath{{field: "x"}}, accPath{{field: "x"}, {field: "y"}}, false},
		{"different order", accPath{{field: "x"}, {field: "y"}}, accPath{{field: "y"}, {field: "x"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathEq(tt.a, tt.b); got != tt.want {
				t.Fatalf("pathEq(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMapValType(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"map[string]int", "int"},
		{"map[int]string", "string"},
		{"map[string]map[int]bool", "map[int]bool"},
		{"map[string][]int", "[]int"},
		{"map[string]", ""},
		{"nope", "?"},
	}
	for _, tt := range tests {
		if got := mapValType(tt.in); got != tt.want {
			t.Fatalf("mapValType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCopyIntMap(t *testing.T) {
	orig := map[string]int{"a": 1, "b": 2}
	clone := copyIntMap(orig)
	if len(clone) != len(orig) {
		t.Fatalf("copyIntMap(%v) = %v, want same length", orig, clone)
	}
	for k, v := range orig {
		if clone[k] != v {
			t.Fatalf("copyIntMap(%v)[%q] = %d, want %d", orig, k, clone[k], v)
		}
	}

	// Mutating the clone must not affect the original.
	clone["a"] = 99
	if orig["a"] != 1 {
		t.Fatalf("mutating clone affected original: %v", orig)
	}

	empty := copyIntMap(nil)
	if len(empty) != 0 {
		t.Fatalf("copyIntMap(nil) = %v, want empty map", empty)
	}
}
