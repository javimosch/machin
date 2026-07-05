package main

import (
	"strings"
	"testing"
)

// TestMultiAssignCommaOkReceive exercises the comma-ok receive case in
// genMultiAssign where a channel receive yields (value, ok bool).
func TestMultiAssignCommaOkReceive(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) go send(c) v, ok := <-c println(v) println(ok) }`,
		`func send(c) { c <- 42 close(c) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignCommaOkReceiveWrongArity rejects comma-ok receive with
// wrong number of variables.
func TestMultiAssignCommaOkReceiveWrongArity(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) v, ok, extra := <-c }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = Check(prog)
	if err == nil || !strings.Contains(err.Error(), "comma-ok receive needs exactly 2 variables") {
		t.Fatalf("expected comma-ok arity error, got %v", err)
	}
}

// TestMultiAssignBlankIdentifier exercises the blank `_` identifier in
// comma-ok receive context (discarding the value).
func TestMultiAssignBlankIdentifier(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan int) go send(c) _, ok := <-c println(ok) }`,
		`func send(c) { c <- 42 close(c) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignBlankAndValue exercises the blank identifier alongside
// a normal variable binding (keeping the value, discarding the ok flag).
func TestMultiAssignBlankAndValue(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { c := make(chan string) go sendstr(c) v, _ := <-c println(v) }`,
		`func sendstr(c) { c <- "hello" close(c) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}
