package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFalsifyProve is the Phase 4 gate: prove mode enumerates a DENSE, fully-
// covered bounded space, so exhausting it clean is an honest bounded proof.
// `proved` requires the whole space enumerated, every input conclusive, and no
// unprovable (infinite-domain) param. Finite domains give an unconditional proof;
// bounded domains a bound-labelled one; float/string/unknown never prove.
func TestFalsifyProve(t *testing.T) {
	falsProve = true
	defer func() { falsProve = false }()

	verdictOf := func(t *testing.T, target string, decls ...string) string {
		prog, err := ParseProgram(decls)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		c, err := Check(prog)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		_, _, verdicts := detectFalsifiableStats(prog, c)
		for _, v := range verdicts {
			if v.Fn == target {
				return v.Verdict
			}
		}
		t.Fatalf("no verdict for %s", target)
		return ""
	}

	cases := []struct {
		name   string
		target string
		want   string
		decls  []string
	}{
		{
			name: "bool-only -> proved (total)", target: "pick", want: "proved",
			decls: []string{`func pick(a,b,c) (r) ensures r == r { if a { return b } return c }`, `func main(){println(str(pick(true,false,true)))}`},
		},
		{
			name: "int + correct ensures -> proved-bounded", target: "myabs", want: "proved-bounded",
			decls: []string{`func myabs(x) (r) ensures r >= 0 { if x < 0 { return 0 - x } return x }`, `func main(){println(str(myabs(-3)))}`},
		},
		{
			name: "[]int correct -> proved-bounded", target: "sumok", want: "proved-bounded",
			decls: []string{`func sumok(xs){total:=0 for _,v:=range xs{total=total+v}return total}`, `func main(){println(str(sumok([]int{1,2})))}`},
		},
		{
			name: "struct of bools -> proved", target: "flags", want: "proved",
			decls: []string{`type F struct{a bool b bool}`, `func flags(f) (r) ensures r == r { if f.a { return f.b } return f.a }`, `func main(){println(str(flags(F{a:true,b:false})))}`},
		},
		{
			name: "struct reaching int -> clean (not overstated)", target: "cfg", want: "clean",
			decls: []string{`type C struct{n int on bool}`, `func cfg(c) (r) ensures r >= 0 { if c.n < 0 { return 0 } return c.n }`, `func main(){println(str(cfg(C{n:1,on:true})))}`},
		},
		{
			name: "float param -> clean (infinite, refuses proof)", target: "fpos", want: "clean",
			decls: []string{`func fpos(x) (r) requires x > 0.0  ensures r > 0.0 { return x }`, `func main(){println(str(fpos(1.0)))}`},
		},
		{
			name: "string param -> clean (infinite)", target: "slen", want: "clean",
			decls: []string{`func slen(s) (r) ensures r >= 0 { return len(s) }`, `func main(){println(str(slen("hi")))}`},
		},
		{
			name: "unknown path (str) -> unknown, never proved", target: "g", want: "unknown",
			decls: []string{`func g(x) (r) ensures r >= 0 { println(str(x))  if x < 0 { return 0 - x } return x }`, `func main(){println(str(g(1)))}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := verdictOf(t, tc.target, tc.decls...); got != tc.want {
				t.Fatalf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestProveFindsDenseBug: dense enumeration catches a bug the sparse sample
// (which only tries {0,1,-1,2,3}) misses — here a div-by-zero only at x=7.
func TestProveFindsDenseBug(t *testing.T) {
	decls := []string{`func f(x){ if x == 7 { return 1/0 } return 0 }`, `func main(){println(str(f(1)))}`}
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	// sparse: misses it.
	if got := detectFalsifiable(prog, c); len(got) != 0 {
		t.Fatalf("sparse should miss the x=7 bug, got %+v", got)
	}
	// prove: dense enumeration finds it.
	falsProve = true
	defer func() { falsProve = false }()
	got := detectFalsifiable(prog, c)
	if len(got) != 1 || got[0].Code != "FALS002" {
		t.Fatalf("prove mode should find the x=7 div-by-zero, got %+v", got)
	}
	if got[0].Bind != "x=7" {
		t.Fatalf("bind = %q, want x=7", got[0].Bind)
	}
}

// TestFalsifyTestOracle covers the cmdFalsifyTest oracle dumper in both modes
// (findings + --prove verdicts) — otherwise exercised only via the shell gate.
func TestFalsifyTestOracle(t *testing.T) {
	dir := t.TempDir()
	// a program with a bug (divmod ÷0) and a proved-bounded function.
	src := "func d(a,b){return a/b}\nfunc myabs(x) (r) ensures r >= 0 { if x < 0 { return 0 - x } return x }\nfunc main(){println(str(d(6,2)),str(myabs(-1)))}\n"
	path := filepath.Join(dir, "p.mfl")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// findings mode: dumps hex finding lines (d has a div-by-zero).
	out := captureStdout(t, func() {
		if err := cmdFalsifyTest([]string{"--program", path}); err != nil {
			t.Fatalf("falsifytest: %v", err)
		}
	})
	if !strings.Contains(out, "|") || out == "" {
		t.Fatalf("findings dump empty:\n%q", out)
	}

	// prove mode: dumps hex fn|verdict lines.
	out = captureStdout(t, func() {
		if err := cmdFalsifyTest([]string{"--program", path, "--prove"}); err != nil {
			t.Fatalf("falsifytest --prove: %v", err)
		}
	})
	if strings.Count(out, "\n") < 2 {
		t.Fatalf("prove verdict dump too short:\n%q", out)
	}
	if falsProve {
		t.Fatal("falsProve must be reset after the oracle runs")
	}

	// errors: no --program, unreadable file, unparseable program.
	if err := cmdFalsifyTest(nil); err == nil {
		t.Fatal("want error for missing --program")
	}
	bad := filepath.Join(dir, "bad.mfl")
	os.WriteFile(bad, []byte("func f(x){return x+}"), 0o644)
	out = captureStdout(t, func() { cmdFalsifyTest([]string{"--program", bad}) })
	if !strings.Contains(out, "parse-error") {
		t.Fatalf("want (parse-error), got %q", out)
	}
}
