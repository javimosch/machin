package main

import (
	"strings"
	"testing"
)

// errFragment is a small MFL program that must fail typechecking with an
// error containing the given substring. Drives genMultiAssign error arms
// that the happy-path fixtures in types_stmts_test.go do not reach.
//
// Each element of src is one top-level MFL declaration (one func, one type);
// this matches the canonical MFL shape that ParseProgram lexes cleanly.
type errFragment struct {
	tag     string
	src     []string
	wantErr string // substring of the expected error string
}

// TestTypesMultiAssignErrors is Phase 4 of the coverage push: it drives
// every distinct `return fmt.Errorf` arm and the most reachable
// `return err` propagation arm of genMultiAssign (types.go lines 1518+).
//
// Unlike TestTypesStmtsCoverage (which logs unknown errors), this test is
// strict: a fixture is expected to fail typecheck with a specific error
// string, and a regression that suppresses the branch (parser guards, a
// silent default, etc.) trips the test. The intent is per-arm coverage.
//
// References in the commit narrative:
//   E0  "assignment to undefined variable"           (line 1528)
//   E1  "comma-ok receive needs exactly 2"          (line 1544)
//   PE2 propagated error from c.chanElem            (line 1551)
//        trigger: typed-recv-from-int (union rejects KNum vs KChan)
//   E3  "X: expected N args, got M"                  (line 1571)
//        variants: zero args, too many args
//   E4  "X returns N values but M are assigned"      (line 1580)
//   E5  "pair: expected N args, got M"               (line 1608)
//   E6  "X returns N values but M are assigned"      (line 1615)
//        variants: one name, three names
//   E7  "N variables but a single value"             (line 1620)
//   E8  "N variables but M values"                   (line 1626)
func TestTypesMultiAssignErrors(t *testing.T) {
	frags := []errFragment{
		// E0 — "=" to undeclared multi-name: the parser sees MultiAssign with
		// Op="=" and Names=[x,y]; env lookup misses; the for-loop body emits
		// the error before the function even reaches the Rhs branching.
		{
			tag: "multi_err_eq_undeclared",
			src: []string{
				`func main(){x, y = 1, 2 println(0)}`,
			},
			wantErr: `assignment to undefined variable`,
		},

		// E1 — comma-ok receive with wrong name count. 3 names on the LHS but
		// Rhs[0] is *Recv — length check fires.
		{
			tag: "multi_err_recv_count",
			src: []string{
				`func main(){ch := make(chan int) a, b, c := <-ch println(a)}`,
			},
			wantErr: `comma-ok receive needs exactly 2`,
		},

		// PE2 — propagated error from c.chanElem. Try to receive from an int
		// literal — the union that promotes the int slot to KChan rejects,
		// chanElem returns its own error, genMultiAssign propagates. Drives
		// the `return err` line at ~1551 (NOT a fmt.Errorf literal).
		{
			tag: "multi_pe_chanelem_int_recv",
			src: []string{
				`func main(){_, ok := <-1 println(ok)}`,
			},
			wantErr: ``, // propagated chanElem error — any error is fine
		},

		// E3 — multiRetBuiltin (http_get) called with the wrong number of
		// arguments. Drives the builtin-arity arm.
		{
			tag: "multi_err_builtin_arity_zero",
			src: []string{
				`func main(){_, _, err := http_get() println(err)}`,
			},
			wantErr: `http_get: expected 1 args, got 0`,
		},
		// E3 variant — too many args instead of too few. Distinct call-site
		// arm but same fmt.Errorf call.
		{
			tag: "multi_err_builtin_arity_too_many",
			src: []string{
				`func main(){_, _, err := http_get("u", "extra") println(err)}`,
			},
			wantErr: `http_get: expected 1 args, got 2`,
		},

		// E4 — multiRetBuiltin correct arity, but destructure count doesn't
		// match the number of return values. Use 2 names to force the parser
		// to produce MultiAssign (single-name := for a multi-return call may
		// produce *AssignStmt and route to a different fmt.Errorf in genCall).
		{
			tag: "multi_err_builtin_destructure",
			src: []string{
				`func main(){_, x := http_get("u") println(x)}`,
			},
			wantErr: `http_get returns 3 values but 2 are assigned`,
		},

		// E5 — user multi-return call with wrong arg count. pair has 1
		// parameter; calling pair() with 0 args triggers this.
		{
			tag: "multi_err_user_arity",
			src: []string{
				`func pair(x){return x, x+1}`,
				`func main(){a, b := pair() println(a)}`,
			},
			wantErr: `pair: expected 1 args, got 0`,
		},

		// E6 — user multi-return call correct arity, wrong destructure count.
		// First variant: too few names for the fn's returns. Use 4 names to
		// force the parser to produce MultiAssign (single-name := for a
		// multi-return call may produce *AssignStmt and route through genCall
		// instead of genMultiAssign).
		{
			tag: "multi_err_user_destructure_four",
			src: []string{
				`func pair(x){return x, x+1}`,
				`func main(){_, _, _, b := pair(1) println(b)}`,
			},
			wantErr: `pair returns 2 values but 4 are assigned`,
		},
		// E6 variant — too many names for the fn's returns.
		{
			tag: "multi_err_user_destructure_three",
			src: []string{
				`func pair(x){return x, x+1}`,
				`func main(){a, b, c := pair(1) println(a)}`,
			},
			wantErr: `pair returns 2 values but 3 are assigned`,
		},

		// E7 — single RHS but multi-name LHS; Rhs[0] is not *Recv and not
		// *Call, so the recv/multiRetBuiltin/user-multi-return branches
		// fall through to the len(st.Names) != 1 check.
		{
			tag: "multi_err_single_rhs_multi_lhs",
			src: []string{
				`func main(){a, b := 1 println(a)}`,
			},
			wantErr: `2 variables but a single value on the right`,
		},

		// E8 — multi-RHS multi-name with mismatched counts. Op ":=" means all
		// Names get fresh slots (so the E0 trap doesn't fire first), and
		// the final len(st.Rhs) != len(st.Names) check fires.
		{
			tag: "multi_err_multi_rhs_mismatch",
			src: []string{
				`func main(){a, b, c := 1, 2 println(a)}`,
			},
			wantErr: `3 variables but 2 values`,
		},
	}

	for _, f := range frags {
		f := f
		t.Run(f.tag, func(t *testing.T) {
			prog, err := ParseProgram(f.src)
			if err != nil {
				t.Fatalf("parse %s: unexpected parser error (fixture malformed): %v", f.tag, err)
			}
			_, err = Check(prog)
			if err == nil {
				t.Fatalf("typecheck %s: expected error, got nil — fixture did not drive the intended genMultiAssign arm", f.tag)
			}
			if f.wantErr != "" && !strings.Contains(err.Error(), f.wantErr) {
				t.Fatalf("typecheck %s: error %q does not contain expected %q", f.tag, err.Error(), f.wantErr)
			}
		})
	}
}
