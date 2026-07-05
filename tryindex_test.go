package main

import "testing"

// TestSliceIndexUseWrongIdxType exercises the tryIndex KSlice branch where
// c.union(iu.idx, c.cInt) fails: indexing a slice with a non-int key.
func TestSliceIndexUseWrongIdxType(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { xs := []int{1, 2, 3} k := "a" println(xs[k]) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil {
		t.Fatal("expected type error indexing a slice with a string key")
	}
}

// TestMapIndexUseWrongKeyType exercises the tryIndex KMap branch where
// c.union(iu.idx, ks) fails: indexing a map[string]int with an int key.
func TestMapIndexUseWrongKeyType(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { m := make(map[string]int) m["a"] = 1 x := m[1] println(x) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil {
		t.Fatal("expected type error indexing map[string]int with an int key")
	}
}

// TestSliceIndexResultTypeMismatch exercises the tryIndex KSlice branch where
// c.union(iu.result, e) fails: the indexed element is later used as a string.
func TestSliceIndexResultTypeMismatch(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { xs := []int{1, 2, 3} y := xs[0] y = "hi" println(y) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil {
		t.Fatal("expected type error reassigning an []int element to a string")
	}
}

// TestMapIndexResultTypeMismatch exercises the tryIndex KMap branch where
// c.union(iu.result, vs) fails: the map value is later used as a string.
func TestMapIndexResultTypeMismatch(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { m := make(map[string]int) m["a"] = 1 y := m["a"] y = "hi" println(y) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err == nil {
		t.Fatal("expected type error reassigning a map[string]int value to a string")
	}
}
