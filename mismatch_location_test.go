package main

import (
	"strings"
	"testing"
)

// A mismatch on a literal argument used to name the first string variable in the
// whole program, because all strings share one type slot (#755).
func TestMismatchIsNotBlamedOnAnUnrelatedVariable(t *testing.T) {
	prog := progFromSrc(t, `
func a() { name := "x"
    return name
}
func main() {
    println(a())
    println(str(byte_at("s", 0)))
}`)
	_, err := CompileToC(prog, false)
	if err == nil {
		t.Fatal("byte_at(string) typechecked")
	}
	msg := err.Error()
	if !strings.Contains(msg, `in "main"`) || !strings.Contains(msg, `byte_at("s", 0)`) || strings.Contains(msg, "name") {
		t.Fatalf("misattributed: %s", msg)
	}
}
