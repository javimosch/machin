package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFalsifyFinds is the Slice 1.1 gate: planted-bug fixtures must yield a
// counterexample with the right code/property/expression, and every emitted
// repro must actually panic under --safe (the finding is a REAL bug); correct
// code must stay unflagged (no false positives).
func TestFalsifyFinds(t *testing.T) {
	cases := []struct {
		name     string
		fn       string
		seed     string
		target   string
		wantCode string // "" => expect no finding
		wantProp string
		wantExpr string
	}{
		{
			name:     "off-by-one index",
			fn:       `func sumbad(xs){total:=0 i:=0 for i<=len(xs){total=total+xs[i] i=i+1}return total}`,
			seed:     `func main(){println(str(sumbad([]int{1,2})))}`,
			target:   "sumbad",
			wantCode: "FALS001",
			wantProp: "index out of range",
			wantExpr: "xs[i]",
		},
		{
			name:     "div by zero on empty average",
			fn:       `func avg(xs){total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}`,
			seed:     `func main(){println(str(avg([]int{1,2})))}`,
			target:   "avg",
			wantCode: "FALS002",
			wantProp: "division by zero",
			wantExpr: "total / len(xs)",
		},
		{
			name:     "mod by (a-b): parens preserved in render",
			fn:       `func wrap(a,b){return 100%(a-b)}`,
			seed:     `func main(){println(str(wrap(1,2)))}`,
			target:   "wrap",
			wantCode: "FALS002",
			wantProp: "division by zero",
			wantExpr: "100 % (a - b)", // precedence-correct rendering
		},
		{
			name:     "index with negative computed offset",
			fn:       `func firstgap(xs){return xs[len(xs)-5]}`,
			seed:     `func main(){println(str(firstgap([]int{1,2})))}`,
			target:   "firstgap",
			wantCode: "FALS001",
			wantProp: "index out of range",
			wantExpr: "xs[len(xs) - 5]",
		},
		{
			name:   "correct guarded average — no finding",
			fn:     `func safeavg(xs){if len(xs)==0{return 0}total:=0 for _,v:=range xs{total=total+v}return total/len(xs)}`,
			seed:   `func main(){println(str(safeavg([]int{1,2})))}`,
			target: "safeavg",
		},
		{
			name:   "correct len-bounded sum — no finding",
			fn:     `func sumok(xs){total:=0 for _,v:=range xs{total=total+v}return total}`,
			seed:   `func main(){println(str(sumok([]int{1,2})))}`,
			target: "sumok",
		},
		{
			name:   "guarded division on scalar — no finding",
			fn:     `func ratio(a,b){if b==0{return 0}return a/b}`,
			seed:   `func main(){println(str(ratio(6,2)))}`,
			target: "ratio",
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

			findings := detectFalsifiable(prog, c)
			var got *falsifyFinding
			for i := range findings {
				if findings[i].Decl == tc.target {
					got = &findings[i]
					break
				}
			}

			if tc.wantCode == "" {
				if got != nil {
					t.Fatalf("false positive: %+v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("no finding for %s; got %+v", tc.target, findings)
			}
			if got.Code != tc.wantCode || got.Prop != tc.wantProp {
				t.Fatalf("code/prop = %s/%q, want %s/%q", got.Code, got.Prop, tc.wantCode, tc.wantProp)
			}
			if got.Expr != tc.wantExpr {
				t.Fatalf("expr = %q, want %q", got.Expr, tc.wantExpr)
			}
			t.Logf("FALSIFIED: %s at `%s` when %s", got.Prop, got.Expr, got.Bind)

			// the counterexample must be a REAL bug: repro panics under --safe.
			repro := reproProgram([]string{tc.fn}, tc.target, got.args)
			verifyReproPanics(t, repro)
		})
	}
}

// TestFalsifyDiagnostic checks the check.go-facing rendering.
func TestFalsifyDiagnostic(t *testing.T) {
	ff := falsifyFinding{Decl: "avg", Code: "FALS002", Prop: "division by zero", Expr: "total / len(xs)", Bind: "xs=[]int{}"}
	d := ff.toDiagnostic()
	if d.Phase != "falsify" || d.Code != "FALS002" || d.Severity != "warning" {
		t.Fatalf("phase/code/sev = %s/%s/%s", d.Phase, d.Code, d.Severity)
	}
	want := "division by zero at `total / len(xs)` when xs=[]int{}"
	if d.Message != want {
		t.Fatalf("message = %q, want %q", d.Message, want)
	}
}

// TestFalsifyOperators drives the interpreter's arithmetic/comparison/bitwise/
// float/string arms and the "unknown" (unmodeled) paths, confirming both that
// bugs across operator families are caught and that unmodeled constructs never
// produce a false positive.
func TestFalsifyOperators(t *testing.T) {
	cases := []struct {
		name  string
		decls []string
		fn    string
		found bool // expect a finding for fn?
	}{
		{
			name:  "float divide by zero",
			decls: []string{`func fdiv(a){return 1.0/a}`, `func main(){println(str(fdiv(2.0)))}`},
			fn:    "fdiv", found: true,
		},
		{
			name: "float arithmetic + comparisons (clean)",
			decls: []string{
				`func fmath(a,b){c:=a+b-a*b if c<b||c>a{return c}return a}`,
				`func main(){println(str(fmath(1.0,2.0)))}`,
			},
			fn: "fmath", found: false,
		},
		{
			name: "string concat + comparisons (clean)",
			decls: []string{
				`func scat(s,t){if s==t{return len(s)}if s<t{return 0}if s>t{return 1}return len(s+t)}`,
				`func main(){println(str(scat("a","b")))}`,
			},
			fn: "scat", found: false,
		},
		{
			name: "bitwise/shift/unary reach a mod-by-zero",
			decls: []string{
				`func bits(a,b){x:=(a&b)|(a^b) y:=x<<1 z:=y>>1 if !(z==0){return z%(a-a)}return -x}`,
				`func main(){println(str(bits(1,2)))}`,
			},
			fn: "bits", found: true,
		},
		{
			name: "while + break + continue reach a div-by-zero",
			decls: []string{
				`func loopy(n){i:=0 s:=0 while i<n{i=i+1 if i==2{continue}if i>10{break}s=s+n/(n-n)}return s}`,
				`func main(){println(str(loopy(3)))}`,
			},
			fn: "loopy", found: true,
		},
		{
			name: "calls another user func -> unknown, no false positive",
			decls: []string{
				`func usescall(x){return x+helper(x)}`,
				`func helper(y){return y*2}`,
				`func main(){println(str(usescall(3)))}`,
			},
			fn: "usescall", found: false,
		},
		{
			name: "struct literal -> unknown, no false positive",
			decls: []string{
				`type Point struct{x int y int}`,
				`func usestruct(){p:=Point{x:1,y:2}return p.x}`,
				`func main(){println(str(usestruct()))}`,
			},
			fn: "usestruct", found: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := ParseProgram(tc.decls)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			c, err := Check(prog)
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			var got *falsifyFinding
			for _, f := range detectFalsifiable(prog, c) {
				if f.Decl == tc.fn {
					g := f
					got = &g
					break
				}
			}
			if tc.found && got == nil {
				t.Fatalf("expected a finding for %s", tc.fn)
			}
			if !tc.found && got != nil {
				t.Fatalf("false positive for %s: %+v", tc.fn, *got)
			}
			if got != nil {
				t.Logf("FALSIFIED %s: %s at `%s` when %s", tc.fn, got.Prop, got.Expr, got.Bind)
			}
		})
	}
}

// TestFalsifyHelpers unit-tests the pure rendering / value / domain helpers.
func TestFalsifyHelpers(t *testing.T) {
	if got := (fval{k: KSlice, sl: []fval{vint(1), vint(2)}}).String(); got != "[]int{1, 2}" {
		t.Fatalf("slice String = %q", got)
	}
	if got := vfloat(1.5).String(); got != "1.5" {
		t.Fatalf("float String = %q", got)
	}
	if got := vstr("hi").String(); got != `"hi"` {
		t.Fatalf("string String = %q", got)
	}
	for k, want := range map[Kind]string{KInt: "int", KFloat: "float", KBool: "bool", KString: "string", KSlice: "slice", KStruct: "struct", KMap: "map"} {
		if got := kindName(k); got != want {
			t.Fatalf("kindName(%v) = %q, want %q", k, got, want)
		}
	}
	// precedence rendering: unary, nested arithmetic, boolean chains, index
	prog, err := ParseProgram([]string{
		`func r(a,b,xs){return -a + b*(a-b) < a || !(b==0) && xs[a+1]==0}`,
		`func main(){println(str(r(1,2,[]int{0})))}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Check(prog); err != nil {
		t.Fatalf("check: %v", err)
	}
	ret := prog.Funcs[0].Body[len(prog.Funcs[0].Body)-1].(*ReturnStmt)
	got := exprStr(ret.Vals[0])
	want := "-a + b * (a - b) < a || !(b == 0) && xs[a + 1] == 0"
	if got != want {
		t.Fatalf("exprStr =\n  %q\nwant\n  %q", got, want)
	}
}

func verifyReproPanics(t *testing.T, repro string) {
	t.Helper()
	bin := "bin/machin"
	if _, e := os.Stat(bin); e != nil {
		t.Skipf("no bin/machin (build it to verify repros): %v", e)
	}
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "repro.mfl")
	if err := os.WriteFile(srcPath, []byte(repro), 0o644); err != nil {
		t.Fatalf("write repro: %v", err)
	}
	out, err := exec.Command(bin, "run", "--safe", srcPath).CombinedOutput()
	if err == nil {
		t.Fatalf("repro did NOT panic — counterexample may be spurious:\n%s", out)
	}
	low := strings.ToLower(string(out))
	if !strings.Contains(low, "out of range") && !strings.Contains(low, "divide") &&
		!strings.Contains(low, "modulo") && !strings.Contains(low, "by zero") {
		t.Fatalf("repro failed but not with the expected trap:\n%s", out)
	}
}
