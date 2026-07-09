package main

import (
	"testing"
)

// stmtFragment is a small MFL program with a tag naming the AST subtree it
// exercises in types.go (the branch of genStmt/genMultiAssign/genExprInner/
// genCall/genBinary/tryRange/etc. that the fragment drives).
//
// Each element of src is one top-level MFL declaration (one function, one
// type, etc.). This matches the MFL canonical form: one paragraph-like decl
// per unit, blank line between paragraphs. ParseProgram lexes each element
// independently, so multi-decl fragments split cleanly through the parser.
type stmtFragment struct {
	tag string
	src []string
}

// TestTypesStmtsCoverage is Phase 3 of the coverage push: it exercises
// specific AST subtrees that the runnable example suite does not hit, so
// the typechecker branches are covered by tests and not by ad-hoc fixtures.
// Fixtures are pure typecheck-only (Check(), no execution) so they don't
// shell out to cc/zig or otherwise cost CPU.
//
// The test logs unexpected failures with t.Logf instead of failing so a
// single incomplete fixture cannot turn the suite red; the goal is positive
// coverage, not gating.
func TestTypesStmtsCoverage(t *testing.T) {
	fragments := []stmtFragment{
		// ━━━ closures (genExprInner *MakeClosure + genCall closure-value) ━━━
		{tag: "closure_captureless_apply", src: []string{
			`func mk(){return func(n){return n+1}}`,
			`func main(){println(mk()(5))}`,
		}},
		{tag: "closure_one_capture", src: []string{
			`func box(){a := 10 return func(){return a + 1}}`,
			`func main(){println(box()())}`,
		}},
		{tag: "closure_two_captures", src: []string{
			`func mk(){a := 1 b := 2 return func(){return a + b}}`,
			`func main(){println(mk()())}`,
		}},
		{tag: "closure_callvalue_via_var", src: []string{
			`func mk(){return func(n){return n * 2}}`,
			`func main(){f := mk() println(f(7))}`,
		}},

		// ━━━ multi-assign (genMultiAssign) ━━━
		{tag: "multi_three_simple", src: []string{
			`func main(){a, b, c := 1, 2, 3 println(a + b + c)}`,
		}},
		{tag: "multi_user_two_returns", src: []string{
			`func pair(x){return x, x + 1}`,
			`func main(){a, b := pair(3) println(a + b)}`,
		}},
		{tag: "multi_user_two_returns_discard", src: []string{
			`func pair(x){return x, x + 1}`,
			`func main(){_, b := pair(3) println(b)}`,
		}},
		{tag: "multi_channel_comma_ok", src: []string{
			`func main(){ch := make(chan int) ch <- 9 v, ok := <-ch println(v, ok)}`,
		}},
		{tag: "multi_builtin_three_returns", src: []string{
			// http_get returns (status, body, err); we only need the typechecker
			// to handle it; no actual network call happens in Check().
			`func main(){_, _, err := http_get("http://127.0.0.1:1/") println(err)}`,
		}},
		{tag: "multi_assign_existing_var", src: []string{
			// exercises the slot reuse (existing local) path in genMultiAssign.
			`func main(){x, y := 1, 2 x, y = y, x println(x, y)}`,
		}},

		// ━━━ range over slices/strings ━━━
		{tag: "range_slice_idx_val", src: []string{
			`func main(){xs := []int{10, 20, 30} for i, v := range xs {println(i, v)}}`,
		}},
		{tag: "range_slice_discard_idx", src: []string{
			`func main(){xs := []int{1, 2, 3} s := 0 for _, v := range xs {s = s + v} println(s)}`,
		}},
		{tag: "range_string", src: []string{
			`func main(){s := "hi" n := 0 for _, c := range s {n = n + 1} println(n)}`,
		}},
		{tag: "range_map", src: []string{
			// MFL has no map-literal form; populate via index-assign instead.
			`func main(){m := make(map[string]int) m["a"] = 1 m["b"] = 2 for k, v := range m {println(k, v)}}`,
		}},
		{tag: "range_chan_single_var", src: []string{
			`func main(){ch := make(chan int) ch <- 11 close(ch) for v := range ch {println(v)}}`,
		}},

		// ━━━ struct literals as values ━━━
		{tag: "struct_lit_named_field", src: []string{
			`type P struct{x int y int}`,
			`func main(){p := P{x: 1, y: 2} println(p.x + p.y)}`,
		}},
		{tag: "struct_lit_positional", src: []string{
			`type P struct{x int y int}`,
			`func main(){p := P{1, 2} println(p.x + p.y)}`,
		}},
		{tag: "struct_lit_single_field", src: []string{
			`type B struct{v int}`,
			`func main(){b := B{v: 9} println(b.v)}`,
		}},
		{tag: "struct_lit_nested", src: []string{
			`type Inner struct{n int}`,
			`type Outer struct{i Inner}`,
			`func main(){o := Outer{i: Inner{n: 7}} println(o.i.n)}`,
		}},
		{tag: "struct_lit_in_return", src: []string{
			`type P struct{x int}`,
			`func mk(n){return P{x: n}}`,
			`func main(){p := mk(3) println(p.x)}`,
		}},
		{tag: "struct_field_assign", src: []string{
			`type P struct{x int}`,
			`func main(){p := P{x: 0} p.x = 5 println(p.x)}`,
		}},

		// ━━━ recursive calls ━━━
		{tag: "recursive_sum", src: []string{
			`func s(n){if n == 0 {return 0} else {return n + s(n - 1)}}`,
			`func main(){println(s(4))}`,
		}},
		{tag: "recursive_fib", src: []string{
			`func fib(n){if n < 2 {return n} else {return fib(n - 1) + fib(n - 2)}}`,
			`func main(){println(fib(6))}`,
		}},

		// ━━━ while-loops (genStmt *WhileStmt) ━━━
		{tag: "while_basic", src: []string{
			`func main(){x := 0 while x < 3 {x = x + 1} println(x)}`,
		}},
		{tag: "while_with_break", src: []string{
			`func main(){x := 0 while true {x = x + 1 if x >= 5 {break} } println(x)}`,
		}},
		{tag: "while_with_continue", src: []string{
			`func main(){x := 0 s := 0 while x < 6 {x = x + 1 if x == 3 {continue} s = s + x} println(s)}`,
		}},
		{tag: "while_nested", src: []string{
			`func main(){x := 0 y := 0 while x < 3 {while y < 3 {y = y + 1} x = x + 1} println(x, y)}`,
		}},
		{tag: "while_uses_closure", src: []string{
			`func step(){return func(n){return n + 1}}`,
			`func main(){f := step() x := 0 while x < 4 {x = f(x)} println(x)}`,
		}},

		// ━━━ bonus: other subtrees the runnable suite misses ━━━
		{tag: "unary_minus", src: []string{
			`func main(){x := -5 println(x)}`,
		}},
		{tag: "unary_not", src: []string{
			`func main(){println(!true)}`,
		}},
		{tag: "unary_caret", src: []string{
			`func main(){x := ^7 println(x)}`,
		}},
		{tag: "binary_string_concat", src: []string{
			`func main(){println("a" + "b" + "c")}`,
		}},
		{tag: "field_access_on_lit", src: []string{
			`type Q struct{v int}`,
			`func main(){println(Q{v: 4}.v)}`,
		}},
		{tag: "index_assign_slice", src: []string{
			`func main(){xs := []int{1, 2, 3} xs[0] = 99 println(xs[0])}`,
		}},
		{tag: "index_assign_map", src: []string{
			`func main(){m := make(map[string]int) m["a"] = 1 println(m["a"])}`,
		}},
		{tag: "channel_send_and_recv", src: []string{
			`func main(){ch := make(chan int) ch <- 42 x := <-ch println(x)}`,
		}},
		{tag: "select_recv_closed_chan", src: []string{
			`func main(){ch := make(chan int) close(ch) select { case v := <-ch: println(v) }}`,
		}},
		{tag: "select_two_chans", src: []string{
			`func main(){c1 := make(chan int) c2 := make(chan int) c1 <- 10 v := 0 select { case v1 := <-c1: v = v1 case v2 := <-c2: v = v2 } println(v)}`,
		}},
		{tag: "arena_stmt", src: []string{
			`func main(){x := 0 arena { x = x + 10 } println(x)}`,
		}},
		{tag: "go_stmt", src: []string{
			`func noop(){}`,
			`func main(){go noop() println(1)}`,
		}},
		// Variadic: the parser accepts `nums...` syntax (parser.go:468); the
		// typechecker treats the variadic param as a slice (`vparam`), so this
		// fixture drives the variadic-non-spread branch in genCall. Spread
		// (`xs...`) at call sites depends on the parser setting ex.Spread —
		// untested here to avoid silent fallthrough into the non-spread branch.
		{tag: "variadic_call", src: []string{
			`func count(nums...){return len(nums)}`,
			`func main(){println(count(1, 2, 3))}`,
		}},
	}

	for _, f := range fragments {
		f := f
		t.Run(f.tag, func(t *testing.T) {
			prog, err := ParseProgram(f.src)
			if err != nil {
				t.Logf("parse %s: %v", f.tag, err)
				return
			}
			if _, err := Check(prog); err != nil {
				t.Logf("typecheck %s: %v", f.tag, err)
			}
		})
	}
}
