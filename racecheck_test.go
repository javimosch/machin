package main

import (
	"strings"
	"testing"
)

// rcCheck parses + typechecks canonical MFL decls and runs the race pass.
func rcCheck(t *testing.T, decls ...string) []raceFinding {
	t.Helper()
	prog, err := ParseProgram(decls)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}
	return detectRaces(prog, c)
}

func rcReport(t *testing.T, title string, fs []raceFinding) {
	t.Helper()
	if len(fs) == 0 {
		t.Logf("  %-34s => CLEAN", title)
		return
	}
	for _, f := range fs {
		t.Logf("  %-34s => RACE [%s] on `%s` in %s(): %s",
			title, f.Kind, f.Root, f.Decl, strings.Join(f.Writers, " || "))
	}
}

// ── RACY ────────────────────────────────────────────────────────────────────

func TestRace_WriteWrite(t *testing.T) {
	fs := rcCheck(t,
		"func worker(xs, id){xs[id]=id*2}",
		"func main(){data:=[]int{0,0,0,0} go worker(data,0) go worker(data,1)}")
	rcReport(t, "write/write two goroutines", fs)
	if len(fs) != 1 || fs[0].Root != "data" || fs[0].Kind != "write/write" {
		t.Fatalf("want 1 write/write race on data, got %+v", fs)
	}
}

func TestRace_ReadWrite(t *testing.T) {
	// goroutine writes the slice while this thread reads it — a genuine data race
	// (index-insensitive, sound). Fixes the spike's read-blindness.
	fs := rcCheck(t,
		"func worker(xs){xs[0]=99}",
		"func main(){data:=[]int{0,0} go worker(data) v:=data[1] print(v)}")
	rcReport(t, "read/write goroutine+main", fs)
	if len(fs) != 1 || fs[0].Kind != "read/write" {
		t.Fatalf("want 1 read/write race, got %+v", fs)
	}
}

func TestRace_Transitive(t *testing.T) {
	fs := rcCheck(t,
		"func poke(ys, k){ys[k]=1}",
		"func worker(xs, id){poke(xs, id)}",
		"func main(){data:=[]int{0,0} go worker(data,0) go worker(data,1)}")
	rcReport(t, "transitive mutation", fs)
	if len(fs) != 1 {
		t.Fatalf("want 1 race via transitive mutation, got %+v", fs)
	}
}

func TestRace_SliceFieldInStruct(t *testing.T) {
	// struct passes by value, but its slice field keeps a shared backing array.
	fs := rcCheck(t,
		"type Bag struct{items []int}",
		"func worker(b, id){b.items[id]=id}",
		"func main(){bag:=Bag{[]int{0,0}} go worker(bag,0) go worker(bag,1)}")
	rcReport(t, "slice field in struct", fs)
	if len(fs) != 1 || fs[0].Root != "bag" {
		t.Fatalf("want 1 race on bag (shared slice field), got %+v", fs)
	}
}

func TestRace_LoopSpawnedWriter(t *testing.T) {
	// a `go` inside a loop = N concurrent instances; one loop-spawned writer of a
	// shared slice races itself, even with no other accessor (soundness — this was
	// a false negative surfaced by corpus validation against machin-healthcheck's
	// `while ... { go check(a[i], ...) }` shape).
	fs := rcCheck(t,
		"func check(url, results, idx){results[idx]=len(url)}",
		"func main(){urls:=[]string{\"a\",\"bb\"} results:=[]int{0,0} i:=0 while i<len(urls){go check(urls[i],results,i) i=i+1}}")
	rcReport(t, "loop-spawned shared writer", fs)
	if len(fs) != 1 || fs[0].Root != "results" || fs[0].Kind != "write/write" {
		t.Fatalf("want 1 write/write race on results (loop multiplicity), got %+v", fs)
	}
}

// ── SAFE ──────────────────────────────────────────────────────────────────

func TestRace_ScalarStructField(t *testing.T) {
	// struct copied by value; a scalar-field write races nothing.
	fs := rcCheck(t,
		"type Box struct{n int}",
		"func worker(b){b.n=99}",
		"func main(){box:=Box{0} go worker(box) go worker(box)}")
	rcReport(t, "scalar struct field (copied)", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (struct copied by value), got %+v", fs)
	}
}

func TestRace_DistinctSlices(t *testing.T) {
	fs := rcCheck(t,
		"func worker(xs, id){xs[id]=id*2}",
		"func main(){a:=[]int{0,0} b:=[]int{0,0} go worker(a,0) go worker(b,1)}")
	rcReport(t, "distinct slices", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (distinct slices), got %+v", fs)
	}
}

func TestRace_ShareByCommunicating(t *testing.T) {
	// the slice is produced inside the goroutine and MOVED out over a channel; the
	// receiver reads only its own received value. No shared mutable arg => clean.
	fs := rcCheck(t,
		"func producer(ch){out:=[]int{1,2,3} ch<-out}",
		"func main(){ch:=make(chan []int) go producer(ch) got:=<-ch print(got[0])}")
	rcReport(t, "share by communicating", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (move via channel), got %+v", fs)
	}
}

func TestRace_ReadOnlyShared(t *testing.T) {
	// two goroutines share a slice but only READ it — no writer, no race.
	fs := rcCheck(t,
		"func reader(xs, id){v:=xs[id] print(v)}",
		"func main(){data:=[]int{0,0} go reader(data,0) go reader(data,1)}")
	rcReport(t, "read-only shared", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (no writer), got %+v", fs)
	}
}

// ── globals (Slice 1.2) ──────────────────────────────────────────────────────

func TestRace_GlobalCounter(t *testing.T) {
	// the canonical shared-counter race: a scalar global is shared memory, so two
	// goroutines incrementing it race even though it's not a slice.
	fs := rcCheck(t,
		"var counter = 0",
		"func incr(){counter=counter+1}",
		"func main(){go incr() go incr() print(counter)}")
	rcReport(t, "global shared counter", fs)
	if len(fs) != 1 || fs[0].Root != "counter" || fs[0].Kind != "write/write" {
		t.Fatalf("want 1 write/write race on global counter, got %+v", fs)
	}
}

func TestRace_GlobalWriteMainRead(t *testing.T) {
	fs := rcCheck(t,
		"var total = 0",
		"func add(n){total=total+n}",
		"func main(){go add(5) x:=total print(x)}")
	rcReport(t, "global goroutine-write + main-read", fs)
	if len(fs) != 1 || fs[0].Kind != "read/write" {
		t.Fatalf("want 1 read/write race on global, got %+v", fs)
	}
}

func TestRace_GlobalInitThenSpawn(t *testing.T) {
	// writing a global BEFORE the first spawn happens-before the goroutines, so
	// readers see a stable value — safe (the common init-then-spawn pattern).
	fs := rcCheck(t,
		"var config = 0",
		"func reader(id){x:=config print(x+id)}",
		"func main(){config=42 go reader(0) go reader(1)}")
	rcReport(t, "global init-then-spawn readers", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (write ordered-before spawn), got %+v", fs)
	}
}

func TestRace_GlobalReadOnly(t *testing.T) {
	fs := rcCheck(t,
		"var base = 100",
		"func reader(id, ch){ch<-base+id}",
		"func main(){ch:=make(chan int) go reader(0,ch) go reader(1,ch) a:=<-ch b:=<-ch print(a+b)}")
	rcReport(t, "global read-only shared", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (no writer), got %+v", fs)
	}
}

// ── move-on-send (Slice 1.2) ─────────────────────────────────────────────────

func TestRace_UseAfterSend(t *testing.T) {
	// sending a slice transfers ownership; mutating it afterward races the receiver.
	fs := rcCheck(t,
		"func producer(ch){out:=[]int{1,2,3} ch<-out out[0]=99}",
		"func main(){ch:=make(chan []int) go producer(ch) got:=<-ch print(got[0])}")
	rcReport(t, "use after send", fs)
	if len(fs) != 1 || fs[0].Kind != "use-after-move" || fs[0].Root != "out" {
		t.Fatalf("want 1 use-after-move on out, got %+v", fs)
	}
}

func TestRace_SendThenDrop(t *testing.T) {
	// send and never touch it again — the safe move, no diagnostic.
	fs := rcCheck(t,
		"func producer(ch){out:=[]int{1,2,3} ch<-out}",
		"func main(){ch:=make(chan []int) go producer(ch) got:=<-ch print(got[0])}")
	rcReport(t, "send then drop", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (value dropped after send), got %+v", fs)
	}
}

// ── happens-before precision (Slice 1.4) ─────────────────────────────────────

func TestRace_PreSpawnSetup(t *testing.T) {
	// filling a buffer BEFORE spawning a worker is ordered-before the goroutine —
	// not a race (was a false positive before 1.4).
	fs := rcCheck(t,
		"func worker(xs){xs[0]=99}",
		"func main(){data:=[]int{0,0} data[0]=5 go worker(data)}")
	rcReport(t, "pre-spawn setup", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (write ordered-before spawn), got %+v", fs)
	}
}

func TestRace_JoinBarrier(t *testing.T) {
	// a goroutine that signals completion (its LAST statement) on a channel the
	// main thread receives establishes happens-before: reading the data after the
	// receive is safe.
	fs := rcCheck(t,
		"func worker(xs, done){xs[0]=99 done<-1}",
		"func main(){data:=[]int{0,0} done:=make(chan int) go worker(data,done) x:=<-done print(data[0]+x)}")
	rcReport(t, "join barrier then read", fs)
	if len(fs) != 0 {
		t.Fatalf("want CLEAN (joined before read), got %+v", fs)
	}
}

func TestRace_JoinSignalBeforeWrite(t *testing.T) {
	// SOUNDNESS: the goroutine signals BEFORE it writes (send is not last), so the
	// receive does NOT establish happens-before on the write — must still flag.
	fs := rcCheck(t,
		"func worker(xs, done){done<-1 xs[0]=99}",
		"func main(){data:=[]int{0,0} done:=make(chan int) go worker(data,done) x:=<-done print(data[0]+x)}")
	rcReport(t, "signal-before-write (must flag)", fs)
	if len(fs) == 0 {
		t.Fatalf("want RACE (send not last -> no valid join), got CLEAN")
	}
}

func TestRace_JoinTooFewReceives(t *testing.T) {
	// SOUNDNESS: two goroutines signal, only one receive — the second is still live
	// when main reads. Must still flag.
	fs := rcCheck(t,
		"func worker(xs, done){xs[0]=99 done<-1}",
		"func main(){data:=[]int{0,0} done:=make(chan int) go worker(data,done) go worker(data,done) x:=<-done print(data[0]+x)}")
	rcReport(t, "too-few-receives (must flag)", fs)
	if len(fs) == 0 {
		t.Fatalf("want RACE (recv < spawn -> no full join), got CLEAN")
	}
}

// ── banner ──────────────────────────────────────────────────────────────────

func TestRace_ZZZBanner(t *testing.T) {
	t.Log("── Slice 1.1: inferred data-race verdicts (no Send/Sync) ──")
	rows := []struct {
		name  string
		decls []string
	}{
		{"RACY  write/write shared slice", []string{
			"func w(xs,i){xs[i]=i}", "func main(){d:=[]int{0,0} go w(d,0) go w(d,1)}"}},
		{"RACY  read/write shared slice", []string{
			"func w(xs){xs[0]=1}", "func main(){d:=[]int{0,0} go w(d) v:=d[1] print(v)}"}},
		{"RACY  shared slice field in struct", []string{
			"type Bag struct{items []int}", "func w(b,i){b.items[i]=i}",
			"func main(){g:=Bag{[]int{0,0}} go w(g,0) go w(g,1)}"}},
		{"SAFE  scalar struct field (copied)", []string{
			"type Box struct{n int}", "func w(b){b.n=1}",
			"func main(){x:=Box{0} go w(x) go w(x)}"}},
		{"SAFE  distinct slices", []string{
			"func w(xs,i){xs[i]=i}", "func main(){a:=[]int{0,0} b:=[]int{0,0} go w(a,0) go w(b,1)}"}},
		{"SAFE  share by communicating", []string{
			"func p(ch){o:=[]int{1} ch<-o}", "func main(){ch:=make(chan []int) go p(ch) g:=<-ch print(g[0])}"}},
	}
	for _, r := range rows {
		fs := rcCheck(t, r.decls...)
		if len(fs) > 0 {
			t.Logf("  [DATA RACE] %-34s %s: %s", r.name, fs[0].Kind, strings.Join(fs[0].Writers, " || "))
		} else {
			t.Logf("  [   ok    ] %-34s CLEAN", r.name)
		}
	}
}
