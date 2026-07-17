package main

import "testing"

// Regression: the falsifier's interpreter must use a named-return helper's actual
// value when inlining a call (callUser). It previously read only retVal — set by an
// explicit `return <e>` — so a call to a named-return function evaluated to zero,
// silently mis-evaluating the caller and MISSING real bugs.
//
// off(xs) returns len(xs) via a NAMED return. f guards the empty case, then indexes
// xs[off(xs)] = xs[len(xs)] — an off-by-one that is out of range for EVERY non-empty
// slice. Detectable only when off's named return is honored (len); folded to 0 it
// becomes a valid xs[0] and the bug vanishes. The falsifier must report FALS001 on f.
func TestFalsifyUsesNamedReturnValue(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func off(xs) (r) { r = len(xs) }`,
		`func f(xs) { if len(xs) == 0 { return 0 }  return xs[off(xs)] }`,
		`func main() { println(str(f([]int{1,2}))) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	var got *falsifyFinding
	for _, f := range detectFalsifiable(prog, c) {
		if f.Decl == "f" && f.Code == "FALS001" {
			g := f
			got = &g
			break
		}
	}
	if got == nil {
		t.Fatal("expected FALS001 on f (xs[off(xs)] off-by-one via a named-return helper)")
	}
	t.Logf("found the OOB a named-return helper's value drives: %s at %q when %s", got.Prop, got.Expr, got.Bind)
}
