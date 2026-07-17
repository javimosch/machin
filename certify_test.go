package main

import (
	"os"
	"path/filepath"
	"testing"
)

// certifyProg is a test helper: parse + check a program and run translation validation.
func certifyProg(t *testing.T, decls ...string) certReport {
	t.Helper()
	nd := make([]string, len(decls))
	for i, d := range decls {
		nd[i] = normalize(d)
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	rep, err := certifyProgram(prog, c, nd)
	if err != nil {
		t.Fatalf("certify: %v", err)
	}
	return rep
}

func verdictOf(rep certReport, fn string) string {
	for _, v := range rep.Verdicts {
		if v.Fn == fn {
			return v.Verdict
		}
	}
	return "(absent)"
}

// TestCertifyHappyPath: the compiler faithfully implements the source, so every
// validatable function (arithmetic, control flow, bitwise, comparison, and an
// interprocedural call) certifies with no divergence.
func TestCertifyHappyPath(t *testing.T) {
	rep := certifyProg(t,
		`func mul(a, b) (r) { r = a * b }`,
		`func clamp(x) (r) { r = x  if x < 0 { r = 0 }  if x > 5 { r = 5 } }`,
		`func bits(a, b) (r) { r = (a & b) | (a ^ b) }`,
		`func cmp(a, b) (r) { r = 0  if a < b { r = 1 }  if a == b { r = 2 } }`,
		`func poly(a, b) (r) { r = mul(a, b) + clamp(a) }`,
		`func main() { println(str(mul(2,3)+clamp(9)+bits(6,3)+cmp(1,1)+poly(2,3))) }`,
	)
	if !rep.OK {
		t.Fatalf("expected a clean program to certify; got %+v", rep.Verdicts)
	}
	for _, fn := range []string{"mul", "clamp", "bits", "cmp", "poly"} {
		v := verdictOf(rep, fn)
		if v != "certified" && v != "certified-bounded" {
			t.Errorf("%s: verdict %q, want certified[-bounded]", fn, v)
		}
	}
	if rep.Checked < 5 {
		t.Errorf("expected >=5 functions checked, got %d", rep.Checked)
	}
}

// TestCertifyDetectsMiscompilation: the detector must fire when the compiled output
// diverges from the source. We drive applyCertResults with a fabricated harness
// result where one probe's compiled value differs — simulating a codegen bug — and
// assert the offending function is flagged with the exact source-vs-compiled values.
func TestCertifyDetectsMiscompilation(t *testing.T) {
	verdicts := []certVerdict{
		{Fn: "good", Verdict: "certified"},
		{Fn: "buggy", Verdict: "certified"},
	}
	vIndex := map[string]int{"good": 0, "buggy": 1}
	probes := []certProbe{
		{fn: "good", call: "good(1, 2)", source: "3"},
		{fn: "buggy", call: "buggy(-3, -3)", source: "9"},
		{fn: "good", call: "good(2, 2)", source: "4"},
	}
	// compiled output: the 2nd probe diverges (compiler said 0 where source said 9).
	got := []string{"3", "0", "4"}

	ok := applyCertResults(verdicts, vIndex, probes, got)
	if ok {
		t.Fatal("expected applyCertResults to report a divergence")
	}
	if verdicts[1].Verdict != "miscompiled" {
		t.Fatalf("buggy: verdict %q, want miscompiled", verdicts[1].Verdict)
	}
	if verdicts[1].Source != "9" || verdicts[1].Compiled != "0" || verdicts[1].Expr != "buggy(-3, -3)" {
		t.Fatalf("miscompilation detail wrong: %+v", verdicts[1])
	}
	if verdicts[0].Verdict != "certified" {
		t.Errorf("good should stay certified, got %q", verdicts[0].Verdict)
	}
}

// TestCertifyNonScalarIsUnknown: a function returning a slice/string isn't validated
// in this slice — it must be reported unknown, never falsely certified.
func TestCertifyNonScalarIsUnknown(t *testing.T) {
	rep := certifyProg(t,
		`func mk(n) (r) { r = []int{n, n} }`,
		`func main() { xs := mk(3)  println(str(len(xs))) }`,
	)
	if v := verdictOf(rep, "mk"); v != "unknown" && v != "partial" {
		t.Errorf("slice-returning mk: verdict %q, want unknown/partial (not certified)", v)
	}
	if !rep.OK {
		t.Errorf("a non-scalar return is unvalidatable, not a miscompilation")
	}
}

// TestCertifyCommand exercises the CLI entry (both text and --json output) end to end
// on a clean program, plus its error paths.
func TestCertifyCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.mfl")
	src := "func sq(a) (r) { r = a * a }\nfunc dec(a) (r) { r = a - 1 }\nfunc main() { println(str(sq(3) + dec(4))) }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdCertify([]string{path}); err != nil {
		t.Fatalf("certify (text): %v", err)
	}
	if err := cmdCertify([]string{"--json", path}); err != nil {
		t.Fatalf("certify --json: %v", err)
	}
	if err := cmdCertify(nil); err == nil {
		t.Fatal("expected an error with no source file")
	}
	if err := cmdCertify([]string{filepath.Join(dir, "does-not-exist.mfl")}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	bad := filepath.Join(dir, "bad.mfl")
	os.WriteFile(bad, []byte("func main() { x := }\n"), 0o644)
	if err := cmdCertify([]string{bad}); err == nil {
		t.Fatal("expected a parse/type error for malformed source")
	}
}

// TestCertifyReportRendering covers every verdict branch of the text renderer and the
// verdict-ranking helper.
func TestCertifyReportRendering(t *testing.T) {
	rep := certReport{
		OK: false,
		Verdicts: []certVerdict{
			{Fn: "a", Verdict: "certified", Tried: 5},
			{Fn: "b", Verdict: "certified-bounded", Tried: 25},
			{Fn: "c", Verdict: "partial", Tried: 10},
			{Fn: "d", Verdict: "unknown"},
			{Fn: "e", Verdict: "miscompiled", Expr: "e(1)", Source: "2", Compiled: "3", Tried: 5},
		},
		Bounds: falsifyBounds{SliceLenMax: 3, IntDomain: []int64{0, 1}},
	}
	printCertReport(rep) // OK=false + every verdict case
	rep.OK = true
	printCertReport(rep) // OK=true summary

	ranks := map[string]int{}
	for _, v := range []string{"miscompiled", "unknown", "partial", "certified-bounded", "certified", "other"} {
		ranks[v] = certRank(v)
	}
	if !(ranks["miscompiled"] > ranks["unknown"] && ranks["unknown"] > ranks["partial"] &&
		ranks["partial"] > ranks["certified-bounded"] && ranks["certified-bounded"] > ranks["certified"] &&
		ranks["certified"] > ranks["other"]) {
		t.Fatalf("certRank ordering wrong: %+v", ranks)
	}
}

// TestCertifyRenderLikeStr covers the scalar renderings and the deferred (non-scalar) case.
func TestCertifyRenderLikeStr(t *testing.T) {
	cases := []struct {
		v    fval
		want string
		ok   bool
	}{
		{vint(-7), "-7", true},
		{vfloat(1.5), "1.5", true},
		{vbool(true), "true", true},
		{vbool(false), "false", true},
		{vstr("hi"), "", false},
	}
	for _, c := range cases {
		got, ok := renderLikeStr(c.v)
		if ok != c.ok || got != c.want {
			t.Errorf("renderLikeStr(%v) = %q,%v want %q,%v", c.v, got, ok, c.want, c.ok)
		}
	}
}
