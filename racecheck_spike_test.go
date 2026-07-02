package main

import (
	"strings"
	"testing"
)

// prog parses canonical MFL function decls into a *Program (reuses the real parser).
func rsProg(t *testing.T, decls ...string) *Program {
	t.Helper()
	p, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func rsReport(t *testing.T, title string, fs []rsFinding) {
	t.Helper()
	if len(fs) == 0 {
		t.Logf("  %s => CLEAN (no data race inferred)", title)
		return
	}
	for _, f := range fs {
		t.Logf("  %s => DATA RACE on `%s` in %s():", title, f.Root, f.Fn)
		for _, w := range f.Writers {
			t.Logf("       - %s", w)
		}
	}
}

// RACY: the same slice handed to two goroutines that both write it. The classic
// data race Rust rejects with Send/Sync — here inferred, no annotations.
func TestRaceSpike_TwoGoroutinesShareSlice(t *testing.T) {
	prog := rsProg(t,
		"func worker(xs, id){xs[id]=id*2}",
		"func main(){data:=[]int{0,0,0,0} go worker(data,0) go worker(data,1)}",
	)
	fs := rsAnalyze(prog)
	rsReport(t, "two-goroutines-share-slice", fs)
	if len(fs) != 1 || fs[0].Root != "data" {
		t.Fatalf("expected 1 race on `data`, got %+v", fs)
	}
	if len(fs[0].Writers) != 2 {
		t.Fatalf("expected 2 concurrent writers, got %d", len(fs[0].Writers))
	}
}

// RACY: spawn a mutating goroutine, then keep mutating the same slice on this thread.
func TestRaceSpike_GoroutinePlusMainWrite(t *testing.T) {
	prog := rsProg(t,
		"func worker(xs){xs[0]=99}",
		"func main(){data:=[]int{0,0} go worker(data) data[1]=7}",
	)
	fs := rsAnalyze(prog)
	rsReport(t, "goroutine-plus-main-write", fs)
	if len(fs) != 1 || fs[0].Root != "data" {
		t.Fatalf("expected 1 race on `data`, got %+v", fs)
	}
}

// RACY through a transitive call: the mutation is one level down.
func TestRaceSpike_TransitiveMutation(t *testing.T) {
	prog := rsProg(t,
		"func poke(ys, k){ys[k]=1}",
		"func worker(xs, id){poke(xs, id)}",
		"func main(){data:=[]int{0,0} go worker(data,0) go worker(data,1)}",
	)
	fs := rsAnalyze(prog)
	rsReport(t, "transitive-mutation", fs)
	if len(fs) != 1 {
		t.Fatalf("expected 1 race via transitive mutation, got %+v", fs)
	}
}

// SAFE: distinct slices per goroutine — no shared root, no race.
func TestRaceSpike_DistinctSlices(t *testing.T) {
	prog := rsProg(t,
		"func worker(xs, id){xs[id]=id*2}",
		"func main(){a:=[]int{0,0} b:=[]int{0,0} go worker(a,0) go worker(b,1)}",
	)
	fs := rsAnalyze(prog)
	rsReport(t, "distinct-slices", fs)
	if len(fs) != 0 {
		t.Fatalf("expected CLEAN (distinct slices), got %+v", fs)
	}
}

// SAFE: a single mutating goroutine; this thread only READS the slice.
func TestRaceSpike_SingleWriter(t *testing.T) {
	prog := rsProg(t,
		"func worker(xs){xs[0]=99}",
		"func main(){data:=[]int{0,0} go worker(data) print(data[1])}",
	)
	fs := rsAnalyze(prog)
	rsReport(t, "single-writer-others-read", fs)
	if len(fs) != 0 {
		t.Fatalf("expected CLEAN (one writer), got %+v", fs)
	}
}

// SAFE: share by communicating — the slice is not passed to a mutating goroutine
// param at all; ownership moves through a channel. No shared mutable arg => clean.
func TestRaceSpike_ShareByCommunicating(t *testing.T) {
	prog := rsProg(t,
		"func producer(ch){out:=[]int{1,2,3} ch<-out}",
		"func main(){ch:=make(chan []int) go producer(ch) got:=<-ch print(got[0])}",
	)
	fs := rsAnalyze(prog)
	rsReport(t, "share-by-communicating", fs)
	if len(fs) != 0 {
		t.Fatalf("expected CLEAN (move via channel), got %+v", fs)
	}
}

// Aggregate banner so `go test -run RaceSpike -v` reads as a single verdict.
func TestRaceSpike_ZZZBanner(t *testing.T) {
	cases := []struct {
		name  string
		decls []string
		race  bool
	}{
		{"RACY  two goroutines share a mutated slice", []string{
			"func worker(xs, id){xs[id]=id*2}",
			"func main(){data:=[]int{0,0,0,0} go worker(data,0) go worker(data,1)}"}, true},
		{"SAFE  distinct slice per goroutine", []string{
			"func worker(xs, id){xs[id]=id*2}",
			"func main(){a:=[]int{0,0} b:=[]int{0,0} go worker(a,0) go worker(b,1)}"}, false},
	}
	t.Log("── inferred data-race verdict (no Send/Sync annotations) ──")
	for _, c := range cases {
		fs := rsAnalyze(rsProg(t, c.decls...))
		got := len(fs) > 0
		mark := "ok"
		if got != c.race {
			mark = "MISMATCH"
		}
		verdict := "CLEAN"
		if got {
			verdict = "DATA RACE: " + strings.Join(fs[0].Writers, " || ")
		}
		t.Logf("  [%s] %s => %s", mark, c.name, verdict)
		if got != c.race {
			t.Fail()
		}
	}
}
