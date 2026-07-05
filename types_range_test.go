package main

import (
	"strings"
	"testing"
)

// TestRangeOverChannelTwoVarsRejected exercises the tryRange KChan branch
// (types.go) that rejects `for k, v := range ch` — a channel range yields a
// single received element, not a key/value pair.
func TestRangeOverChannelTwoVarsRejected(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) for k, v := range c { println(k) println(v) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "range over a channel takes a single variable") {
		t.Fatalf("expected channel range arity error, got %v", err)
	}
}

// TestRangeOverChannelSingleVar exercises the tryRange KChan success branch
// with a single loop variable bound to the channel's element type.
func TestRangeOverChannelSingleVar(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) go send(c) for v := range c { println(v) } }`,
		`func send(c) { c <- 1 close(c) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestRangeOverSliceKeyOnly exercises tryRange KSlice branch with single
// loop variable (key only, no value). This covers the hasVal=false path.
func TestRangeOverSliceKeyOnly(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := []int{1, 2, 3} for i := range s { println(i) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestRangeOverSliceKeyValue exercises tryRange KSlice branch with both
// key and value (index and element).
func TestRangeOverSliceKeyValue(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := []int{1, 2, 3} for i, v := range s { println(i) println(v) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestRangeOverStringKeyOnly exercises tryRange KString branch with
// single loop variable (rune index only, no rune value).
func TestRangeOverStringKeyOnly(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := "hello" for i := range s { println(i) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestRangeOverStringKeyValue exercises tryRange KString branch with both
// key and value (rune index and rune character).
func TestRangeOverStringKeyValue(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { s := "hello" for i, c := range s { println(i) println(c) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestRangeOverMapKeyOnly exercises tryRange KMap branch with
// single loop variable (key only, no map value).
func TestRangeOverMapKeyOnly(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { m := make(map[string]int) m["a"] = 1 for k := range m { println(k) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestRangeOverMapKeyValue exercises tryRange KMap branch with both
// key and value (map key and mapped value).
func TestRangeOverMapKeyValue(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { m := make(map[string]int) m["a"] = 1 for k, v := range m { println(k) println(v) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}
