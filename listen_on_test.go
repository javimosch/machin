package main

import (
	"strings"
	"testing"
)

// listen_on exists because listen(port) hardcodes INADDR_ANY: a server told to
// serve 127.0.0.1 was reachable from the whole network and had no way to opt
// out. essaim's torrent daemon accepted --host 127.0.0.1, logged it, and bound
// 0.0.0.0.
func TestListenOnBindsAndRejects(t *testing.T) {
	src := `func main() {
		lo := listen_on("127.0.0.1", 0)
		bad := listen_on("no.such.host.invalid", 0)
		println(str(lo >= 0))
		println(str(bad == 0 - 1))
		close(lo)
	}`
	fn, err := ParseFunc(normalize(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := RunCaptured(&Program{Funcs: []*FuncDecl{fn}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := strings.TrimSpace(out), "true\ntrue"; got != want {
		t.Fatalf("listen_on = %q, want %q", got, want)
	}
}

// (string, int) -> int; the checker must agree, and arity is enforced.
func TestListenOnTypes(t *testing.T) {
	fn, _ := ParseFunc(normalize(`func main() { println(str(listen_on("127.0.0.1", 0))) }`))
	if _, err := Check(&Program{Funcs: []*FuncDecl{fn}}); err != nil {
		t.Fatalf("listen_on should type-check (string,int -> int): %v", err)
	}
	bad, _ := ParseFunc(normalize(`func main() { println(str(listen_on(8080))) }`))
	if _, err := Check(&Program{Funcs: []*FuncDecl{bad}}); err == nil {
		t.Fatal("listen_on with 1 arg should be rejected")
	}
}
