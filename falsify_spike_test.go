package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFalsifySpike is the Phase 0 de-risk: plant real bugs in small MFL
// functions and confirm the bounded checker (a) finds a concrete counterexample,
// (b) renders the offending expression, (c) runs fast, and (d) the generated
// repro actually panics when built with --safe.
func TestFalsifySpike(t *testing.T) {
	cases := []struct {
		name    string
		fn      string // the target function (untyped params, MFL-style)
		target  string
		seed    string // a main() calling target with representative-typed args
		wantYes bool
		wantVia string
	}{
		{
			name:    "off-by-one index (classic agent bug)",
			fn:      `func sumbad(xs){total:=0 i:=0 for i<=len(xs){total=total+xs[i] i=i+1}return total}`,
			target:  "sumbad",
			seed:    `func main(){println(str(sumbad([]int{1,2})))}`,
			wantYes: true,
			wantVia: "index out of range",
		},
		{
			name:    "div by zero on empty average",
			fn:      `func avg(xs){total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}`,
			target:  "avg",
			seed:    `func main(){println(str(avg([]int{1,2})))}`,
			wantYes: true,
			wantVia: "division by zero",
		},
		{
			name:    "mod by (a-b) that can be zero",
			fn:      `func wrap(a,b){return 100%(a-b)}`,
			target:  "wrap",
			seed:    `func main(){println(str(wrap(1,2)))}`,
			wantYes: true,
			wantVia: "division by zero",
		},
		{
			name:    "correct guarded average — must NOT flag",
			fn:      `func safeavg(xs){if len(xs)==0{return 0}total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}`,
			target:  "safeavg",
			seed:    `func main(){println(str(safeavg([]int{1,2})))}`,
			wantYes: false,
		},
		{
			name:    "correct sum — must NOT flag",
			fn:      `func sumok(xs){total:=0 for _,v:=range xs{total=total+v}return total}`,
			target:  "sumok",
			seed:    `func main(){println(str(sumok([]int{1,2})))}`,
			wantYes: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := ParseProgram([]string{tc.fn, tc.seed})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			c, err := Check(prog)
			if err != nil {
				t.Fatalf("check: %v", err)
			}

			res := falsify(prog, c, tc.target)
			t.Logf("tried=%d elapsed=%v unknown=%v found=%v",
				res.tried, res.elapsed, res.unknown, res.found)

			if res.found != tc.wantYes {
				t.Fatalf("found=%v, want %v", res.found, tc.wantYes)
			}
			if !tc.wantYes {
				return
			}
			if !strings.Contains(res.ce.prop, tc.wantVia) {
				t.Fatalf("property = %q, want substring %q", res.ce.prop, tc.wantVia)
			}
			t.Logf("COUNTEREXAMPLE:\n%s", res.ce.String())

			repro := reproProgram([]string{tc.fn}, tc.target, res.ce)
			t.Logf("REPRO:\n%s", repro)
			verifyRepro(t, repro)
		})
	}
}

// verifyRepro writes the repro, builds it with --safe, runs it, and asserts a
// runtime panic — i.e. the counterexample is a REAL bug, not a checker artifact.
func verifyRepro(t *testing.T, repro string) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "repro.mfl")
	if err := os.WriteFile(srcPath, []byte(repro), 0o644); err != nil {
		t.Fatalf("write repro: %v", err)
	}
	bin := "bin/machin"
	if _, e := os.Stat(bin); e != nil {
		t.Skipf("no bin/machin to verify repro (build it first): %v", e)
	}
	out, err := exec.Command(bin, "run", "--safe", srcPath).CombinedOutput()
	t.Logf("repro run output: %q err=%v", string(out), err)
	if err == nil {
		t.Fatalf("repro did NOT panic — counterexample may be spurious:\n%s", out)
	}
	low := strings.ToLower(string(out))
	if !strings.Contains(low, "index out of range") && !strings.Contains(low, "divide") &&
		!strings.Contains(low, "division") && !strings.Contains(low, "by zero") &&
		!strings.Contains(low, "panic") {
		t.Fatalf("repro failed but not with the expected trap:\n%s", out)
	}
}
