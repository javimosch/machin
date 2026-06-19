package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// runMFL encodes readable function sources to the machine-first form and runs them.
func runMFL(t *testing.T, funcs ...string) string {
	t.Helper()
	in := NewInterp()
	for _, f := range funcs {
		// round-trip through base64 to exercise the real machine-first path
		enc := base64.StdEncoding.EncodeToString([]byte(normalize(f)))
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("b64: %v", err)
		}
		fn, err := ParseFunc(string(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		if err := in.Register(fn); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	out, err := in.Run()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

func TestArithmetic(t *testing.T) {
	got := runMFL(t, `func main() { println(2 + 3 * 4) }`)
	if got != "14\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRecursionAndLoop(t *testing.T) {
	got := runMFL(t,
		`func fib(n) { if n < 2 { return n } return fib(n-1) + fib(n-2) }`,
		`func main() { println(fib(10)) }`)
	if got != "55\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStringsAndBools(t *testing.T) {
	got := runMFL(t, `func main() { println("a" + "b", 1 == 1, !false) }`)
	if got != "ab true true\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFloatPromotion(t *testing.T) {
	got := runMFL(t, `func main() { println(7 / 2, 7.0 / 2) }`)
	if got != "3 3.5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	src := "func add(a, b) {\n    return a + b\n}"
	fn, err := ParseFunc(normalize(src))
	if err != nil {
		t.Fatal(err)
	}
	pretty := prettyFunc(fn)
	if !strings.Contains(pretty, "func add(a, b)") || !strings.Contains(pretty, "return (a + b)") {
		t.Fatalf("pretty mismatch: %s", pretty)
	}
}

func TestSplitFunctions(t *testing.T) {
	src := "func a() { return 1 }\n\nfunc b() { return 2 }\n"
	fns, err := splitFunctions(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fns) != 2 {
		t.Fatalf("expected 2 funcs, got %d", len(fns))
	}
}
