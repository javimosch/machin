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
