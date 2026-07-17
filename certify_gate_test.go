package main

import "testing"

// TestCertifyCodegenPaths is the continuous-self-certification gate: a battery of dense,
// certify-friendly programs that exercise a wide span of codegen paths (arithmetic,
// comparisons, bitwise, shifts, boolean short-circuit, guarded modulo, control flow,
// bounded recursion, slice literal/append/index/len/range, structs, strings, and
// interprocedural calls). Every one must certify with NO miscompilation — this fires if a
// codegen change ever makes a validatable function diverge from its source.
func TestCertifyCodegenPaths(t *testing.T) {
	programs := [][]string{
		{`func arith(a, b) (r) { r = a + b - a * b + (a - b) }`,
			`func main() { println(str(arith(2, 3))) }`},
		{`func bits(a, b) (r) { r = ((a & b) | (a ^ b)) + (a << 1) + (b >> 1) }`,
			`func main() { println(str(bits(6, 3))) }`},
		{`func cmp(a, b) (r) { r = 0  if a < b { r = r + 1 }  if a <= b { r = r + 2 }  if a > b { r = r + 4 }  if a >= b { r = r + 8 }  if a == b { r = r + 16 }  if a != b { r = r + 32 } }`,
			`func main() { println(str(cmp(1, 2))) }`},
		{`func logic(a, b) (r) { r = 0  if a > 0 && b > 0 { r = 1 }  if a > 0 || b > 0 { r = r + 10 } }`,
			`func main() { println(str(logic(1, -1))) }`},
		{`func modrem(a) (r) { r = 0  if a != 0 { r = 100 % a + 100 / a } }`,
			`func main() { println(str(modrem(3))) }`},
		{`func fib(n) (r) { r = n  if n > 1 { r = fib(n - 1) + fib(n - 2) } }`,
			`func main() { println(str(fib(3))) }`},
		{`func build(n) (r) { r = []int{}  i := 0  for i < n { r = append(r, i * i)  i = i + 1 } }`,
			`func main() { println(str(len(build(3)))) }`},
		{`func total(xs) (r) { r = 0  for _, v := range xs { r = r + v } }`,
			`func main() { println(str(total([]int{1, 2, 3}))) }`},
		{`func at(xs, i) (r) { r = -1  if i >= 0 { if i < len(xs) { r = xs[i] } } }`,
			`func main() { println(str(at([]int{5, 6}, 1))) }`},
		{`type Pt struct { x int  y int }`,
			`func mk(a, b) (r) { r = Pt{a, b} }`,
			`func main() { println(str(mk(1, 2).x)) }`},
		{`func label(a) (r) { r = "zero"  if a > 0 { r = "pos" }  if a < 0 { r = "neg" } }`,
			`func main() { println(label(3)) }`},
	}
	for _, decls := range programs {
		rep := certifyProg(t, decls...)
		if !rep.OK {
			t.Errorf("codegen-path battery: a program failed to certify: %+v", rep.Verdicts)
		}
		anyValidated := false
		for _, v := range rep.Verdicts {
			if v.Verdict == "miscompiled" {
				t.Fatalf("MISCOMPILATION on a codegen-path fixture: %+v", v)
			}
			if v.Verdict == "certified" || v.Verdict == "certified-bounded" {
				anyValidated = true
			}
		}
		if !anyValidated {
			t.Errorf("codegen-path program validated nothing (decls[0]=%q)", decls[0])
		}
	}
}
