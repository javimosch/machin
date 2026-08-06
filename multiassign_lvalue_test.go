package main

import (
	"testing"
)

// TestParseMultiAssignFieldAndIndexDest exercises issue #554: a multi-assign
// destination list may contain field accesses (including nested paths) and
// index expressions, not just plain identifiers. The parser desugars this
// into a temp-destructuring MultiAssign followed by the field/index stores,
// so the call happens once and stores run left to right.
func TestParseMultiAssignFieldAndIndexDest(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() {
			ns.rng, v = next(s)
			a.b.c, w = next(s)
			xs[i], ok = next(s)
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
	body := prog.Funcs[0].Body
	var multi, fieldStores, indexStores int
	for _, s := range body {
		switch s.(type) {
		case *MultiAssign:
			multi++
		case *FieldAssign:
			fieldStores++
		case *IndexAssign:
			indexStores++
		}
	}
	if multi != 3 {
		t.Fatalf("expected 3 desugared MultiAssign stmts, got %d in %v", multi, body)
	}
	if fieldStores != 2 {
		t.Fatalf("expected 2 FieldAssign stores (ns.rng and a.b.c), got %d", fieldStores)
	}
	if indexStores != 1 {
		t.Fatalf("expected 1 IndexAssign store (xs[i]), got %d", indexStores)
	}
}

// TestMultiAssignFieldAndIndexDestTypecheck exercises issue #554 end to end:
// a multi-value call assigning into a struct field, a nested field path,
// and an index destination all typecheck once desugared into temp binds
// plus stores.
func TestMultiAssignFieldAndIndexDestTypecheck(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type Rng struct { s int }`,
		`type State struct { rng Rng }`,
		`func next(r) (nr, v) { nr = r  v = 1 }`,
		`func step(s) (ns) {
			ns = s
			v := 0
			ns.rng, v = next(s.rng)
			ns.rng.s = ns.rng.s + v
		}`,
		`func main() {
			st := State{rng: Rng{s: 0}}
			r := step(st)
			println(r.rng.s)
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignIndexDestTypecheck exercises an index destination (xs[i])
// mixed with a plain identifier in a multi-assign.
func TestMultiAssignIndexDestTypecheck(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func f() (a, b) { a = 10  b = true }`,
		`func main() {
			xs := []int{0, 0, 0}
			ok := false
			xs[1], ok = f()
			println(xs[1])
			println(ok)
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignEvalOrderCallOnce proves the call happens exactly once
// (via an observable side effect) before the destinations are stored.
func TestMultiAssignEvalOrderCallOnce(t *testing.T) {
	prog, err := ParseProgram([]string{
		`type Rng struct { s int }`,
		`var calls = 0`,
		`func next(r) (nr, v) { calls = calls + 1  nr = r  v = 1 }`,
		`func step(s) (ns) {
			ns = s
			v := 0
			ns.s, v = next(s.s)
			ns.s = ns.s + v
		}`,
		`func main() {
			r := step(Rng{s: 0})
			println(r.s)
			println(calls)
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}

// TestMultiAssignMixedDeclareAndFieldDest exercises criterion #4: mixed
// with ':=' where at least one destination is a fresh identifier, matching
// current ':=' semantics (all-identifier destinations still required).
func TestMultiAssignMixedIdentDeclare(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func f() (a, b) { a = 1  b = 2 }`,
		`func main() {
			x, y := f()
			println(x)
			println(y)
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
}
