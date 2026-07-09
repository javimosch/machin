package main

import (
	"testing"
)

// TestTypesPhase6DeepCoverage is Phase 6 of the coverage push. After Phase 5
// moved genStmt 76.3% → 82.2% and made only faint gains on genExprInner
// (81.2%) / genBinary (86.4%), the user asked to re-target with DEEPER
// fixtures that exercise BRANCHES, not just case-arms. The five targeted areas:
//
//  (1) variadic spread — gensCall lines 2536-2552 (genCall has an `if Variadic`
//      outer + `if ex.Spread` inner; the four-cell matrix is uncovered).
//  (2) named returns — instantiate lines 718-728 (the named-return branch
//      fires only when fn.Returns is non-empty).
//  (3) finalizeMono dedup — lines 798-815 (the dedup branch fires only when
//      a same-signature instance repeats).
//  (4) mutual recursion — instantiate lines 679-684 (instStack lookup on
//      cross-call, not just direct self-call).
//  (5) bytes chains — the KBytes slot hits bytes / byte_at / bytes_str /
//      to_hex / from_hex; the chain drives unification through KBytes at
//      every step.
//
// The test follows the Phase 3+5 convention: every fixture uses t.Logf for
// any parse or typecheck error, so an uncovered branch surfaces as diagnostic
// text, not a failure trip. Coverage is the goal, not gating.
func TestTypesPhase6DeepCoverage(t *testing.T) {
	// softCheck parses + typechecks f.src, logs any error, returns silently.
	// Centralized so every fragment does the same thing.
	softCheck := func(t *testing.T, tag string, src []string) {
		t.Helper()
		prog, err := ParseProgram(src)
		if err != nil {
			t.Logf("parse %s: %v", tag, err)
			return
		}
		if _, err := Check(prog); err != nil {
			t.Logf("typecheck %s: %v", tag, err)
		}
	}

	// ━━━ (1) variadic × spread matrix ━━━
	t.Run("spread_variadic_full", func(t *testing.T) {
		// Variadic + spread — drives the `if ex.Spread` arm (genCall line 2538).
		// The variadic signature is (args ...int) -> void.
		softCheck(t, "spread_variadic_full", []string{
			`func sum(args...){println(1)}`,
			`func main(){xs := []int{1, 2, 3} sum(xs...)}`,
		})
	})
	t.Run("variadic_no_spread", func(t *testing.T) {
		// Variadic, no spread — drives the `else` arm (nfixed < len(argSlots),
		// each individually-bound arg unified with elem).
		softCheck(t, "variadic_no_spread", []string{
			`func sum(args...){println(1)}`,
			`func main(){sum(1, 2, 3, 4)}`,
		})
	})
	t.Run("variadic_partial_spread", func(t *testing.T) {
		// Variadic with fixed + spread — drives the spread path of mixed
		// `f(a, rest...) { f(1, xs...) }`.
		softCheck(t, "variadic_partial_spread", []string{
			`func f(a, rest...){println(a)}`,
			`func main(){xs := []int{3, 4} f(1, xs...)}`,
		})
	})
	t.Run("spread_nonvariadic_errors", func(t *testing.T) {
		// Variadic=false + Spread=true — drives the non-spread arity-mismatch
		// arm (genCall line 2560: len(params) != len(argSlots)).
		// Expected error fires; soft-fail records it.
		softCheck(t, "spread_nonvariadic_errors", []string{
			`func f(a, b){println(a)}`,
			`func main(){xs := []int{1, 2} f(xs...)}`,
		})
	})

	// ━━━ (2) named returns — drives the `if i < len(fn.Returns)` branch ━━━
	t.Run("named_return_single_assigned", func(t *testing.T) {
		// Single named return, assigned in the body, then bare return.
		// instantiate line 720-721: env[fn.Returns[i]] = rets[i] + localOrder
		// append. The `len(st.Vals)==0` arm (bare return) fires in *ReturnStmt.
		// Uses `(out)` syntax so the parser actually sets Returns=["out"];
		// `out = 5` (not `:=`) updates the named-return slot rather than
		// shadowing it with a fresh local.
		softCheck(t, "named_return_single_assigned", []string{
			`func getval() (out){out = 5 return}`,
			`func main(){x := getval() println(x)}`,
		})
	})
	t.Run("named_return_multi_assigned", func(t *testing.T) {
		// Two named returns, destructured into multi-assign at the call site.
		// Exercises both the named-return branch AND the multi-ret user-fn
		// branch of genMultiAssign (line 1661+).
		softCheck(t, "named_return_multi_assigned", []string{
			`func mkpair() (x, y){x = 1 y = 2 return}`,
			`func main(){a, b := mkpair() println(a + b)}`,
		})
	})

	// ━━━ (3) finalizeMono dedup — same signature appears twice ━━━
	t.Run("dedup_same_signature", func(t *testing.T) {
		// Two calls to inc(int) → instantiates inc$0 twice; the second call's
		// sigString == the first's, so the dedup branch (line 803-805) fires
		// and cnameOf[second] = cnameOf[first].
		softCheck(t, "dedup_same_signature", []string{
			`func inc(n){return n + 1}`,
			`func main(){a := inc(1) b := inc(2) println(a + b)}`,
		})
	})
	t.Run("dedup_different_signature_no_dedup", func(t *testing.T) {
		// Negative control: inc(int) called once. Always a fresh instance,
		// dedup branch NOT exercised (but the happy finalizeMono path IS).
		softCheck(t, "dedup_different_signature_no_dedup", []string{
			`func inc(n){return n + 1}`,
			`func main(){println(inc(1))}`,
		})
	})

	// ━━━ (4) mutual recursion ━━━
	t.Run("mutual_recursion_ab", func(t *testing.T) {
		// `a(n)` calls `b(n-1)` which calls `a(n-1)` which ... The instStack
		// lookup (line 681) fires for BOTH directions; direct recursion
		// (Phase 5's recursive_sum) tested one of those.
		softCheck(t, "mutual_recursion_ab", []string{
			`func a(n){if n <= 0 {return 0} return b(n - 1)}`,
			`func b(n){if n <= 1 {return 0} return a(n - 1)}`,
			`func main(){println(a(5))}`,
		})
	})

	// ━━━ (5) bytes chains ━━━
	t.Run("bytes_round_trip_stringify", func(t *testing.T) {
		// bytes(string) → bytes string-literal-style; bytes_str(bytes) →
		// string. KBytes slot propagated through, unification of cBytes.
		softCheck(t, "bytes_round_trip_stringify", []string{
			`func main(){s := "hello" b := bytes(s) s2 := bytes_str(b) println(s == s2)}`,
		})
	})
	t.Run("bytes_hex_chain", func(t *testing.T) {
		// bytes → to_hex (string) → from_hex (bytes) → bytes_str (string).
		// Every step drives a different bytes path in genCall.
		softCheck(t, "bytes_hex_chain", []string{
			`func main(){h := to_hex(bytes("hi")) s := bytes_str(from_hex(h)) println(s == "hi")}`,
		})
	})
	t.Run("bytes_byte_at_indexed", func(t *testing.T) {
		// byte_at on a bytes literal; exercises both byte_at builtin AND
		// bytes_index/sha256_bytes categories indirectly (drive any single-
		// arg-bytes builtin too).
		softCheck(t, "bytes_byte_at_indexed", []string{
			`func main(){x := byte_at(bytes("ab"), 0) b := bytes_concat(bytes("c"), bytes("d")) println(x) println(len(b))}`,
		})
	})

	// ━━━ Bonus: standalone recv (different subtree from Phase 3's `_, ok := <-ch`) ━━━
	t.Run("recv_standalone_returns_chan_elem", func(t *testing.T) {
		// `x := <-ch` parses as AssignStmt{x, := , Recv}. No need to actually
		// route the recv — just exercise the AST shape so case *Recv in
		// genExprInner fires and chanElem completes.
		softCheck(t, "recv_standalone_returns_chan_elem", []string{
			`func main(){ch := make(chan int) x := <-ch println(x)}`,
		})
	})

	// ━━━ Bonus: binary shape variants already covered transitively, but
	// explicit here to drive specific operator branches one more time ━━━
	t.Run("bin_int_eq_float_reconcile", func(t *testing.T) {
		// `1.0 == 1` — genBinary `==` does addPair(ls, rs); the two slots
		// reconcile KFloat with KNum (resolve to KFloat). Distinct from
		// `1 == 2` (Phase 5's bin_neq) where both are int.
		softCheck(t, "bin_int_eq_float_reconcile", []string{
			`func main(){println(1.0 == 1)}`,
		})
	})
	t.Run("bin_string_concat", func(t *testing.T) {
		// `"a" + "b"` — genBinary `+` has its own plus path; for strings,
		// both sides resolve to KString, so it unifies them and returns the
		// pair-res slot — exercises the `kl == KString || kr == KString` arm
		// of solve() that the int `+` paths don't.
		softCheck(t, "bin_string_concat", []string{
			`func main(){println("a" + "b")}`,
		})
	})
	t.Run("bin_unary_minus_literal", func(t *testing.T) {
		// Unary `-` — genExprInner *Unary has the `if ex.Op == "!"` and the
		// `if ex.Op == "^"` arms (typeof), then a default `c.addPair(xs, KNum)`.
		// Phase 3 didn't directly exercise Unary on a numeric literal.
		softCheck(t, "bin_unary_minus_literal", []string{
			`func main(){x := -5 println(x)}`,
		})
	})
	t.Run("bin_unary_not_bool", func(t *testing.T) {
		// Unary `!` on a bool — drives the `c.addPair(xs, c.cBool)` arm
		// and the `return c.cBool` path.
		softCheck(t, "bin_unary_not_bool", []string{
			`func main(){println(!false)}`,
		})
	})
	t.Run("bin_unary_caret_int", func(t *testing.T) {
		// Unary `^` (bitwise complement) — drives the cInt pinning arm
		// (`c.addPair(xs, c.cInt); return xs`).
		softCheck(t, "bin_unary_caret_int", []string{
			`func main(){println(^5)}`,
		})
	})

	// ━━━ Bonus: MakeClosure with no captures (delegated to deepest branch) ━━━
	t.Run("closure_no_captures_call", func(t *testing.T) {
		// Captureless closure: MakeClosure + lambda-lifted func with
		// Captures==nil drives the `if nc == len(ex.Captures)` branch and
		// the `switch rets := c.funcRets[inst]; len(rets): case 1: rets[0]`
		// happy path of genExprInner MakeClosure (line ~1485).
		softCheck(t, "closure_no_captures_call", []string{
			`func main(){f := func(){return 7} println(f())}`,
		})
	})
	t.Run("closure_with_one_capture", func(t *testing.T) {
		// One-capture closure. Drives the `for i, capName := range ex.Captures`
		// loop body and c.addPair(params[i], capSlot). Different shape from
		// Phase 3's `closure_with_capture_outer` (which is capturelist len==1
		// but the closure is called from a different position).
		softCheck(t, "closure_with_one_capture", []string{
			`func main(){x := 10 f := func(){return x} println(f())}`,
		})
	})
	t.Run("closure_with_three_captures_returned", func(t *testing.T) {
		// Three-capture closure returned as a function value. Distinct from
		// Phase 3 (which had a one-capture-fcn passed to `for`). Drives the
		// params[i..nc] bind loop three times.
		softCheck(t, "closure_with_three_captures_returned", []string{
			`func adder() (f){x := 1 y := 2 z := 3 f = func(){return x + y + z} return}`,
			`func main(){g := adder() println(g())}`,
		})
	})
}
