package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Without -fwrapv, gcc -O2 treats signed overflow as UB and is free to fold
// this FNV-1a-style multiply chain to a constant (see issue #547). Building
// through BuildBinary (the real -O2 native path, not the -O0 interpreter/run
// path) must produce the exact two's-complement wrapped results.
func TestFwrapvSignedOverflowWraps(t *testing.T) {
	bin, err := os.CreateTemp("", "mfl-fwrapv-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())

	fnv := `func fnv64(h, v) (o) {
    o = h
    x := v
    i := 0
    while i < 8 {
        o = (o ^ (x & 0xFF)) * 0x100000001B3
        x = x >> 8
        i = i + 1
    }
}`
	main := `func main() {
    h := 0x4BF29CE484222325
    println(str(fnv64(h, 0)) + " " + str(fnv64(h, 4242)) + " " + str(fnv64(fnv64(h, 0), 777)))
}`
	if err := BuildBinary(&Program{Funcs: parseFuncs(t, fnv, main)}, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	want := "2938590176187398597 9125914303777821415 3527166882971247581"
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("signed overflow not wrapping two's-complement: got %q, want %q", got, want)
	}
}
