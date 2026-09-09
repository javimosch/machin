package main

import (
	"strings"
	"testing"
)

// #658: a `bytes` value crossing a channel (or passed to a goroutine) must be
// deep-copied like a string. Before the fix the element was memcpy'd as a scalar,
// so its data pointer kept pointing into the sender goroutine's arena; once that
// goroutine exited the first bytes came back as free-list garbage. Each case
// builds the payload in a goroutine that then exits, churns the allocator, and
// checks every byte.

const chanBytesWorker = `func worker(ch, n) { parts := []string{}  i := 0  while i < n { parts = append(parts, "0100")  i = i + 1 }  ch <- from_hex(join(parts, "")) }`
const chanBytesChurn = `func churn() (s) { s = ""  i := 0  while i < 4000 { s = s + "x"  i = i + 1 }  return }`
const chanBytesCheck = `func check(b, n) (bad) { bad = 0  if len(b) != n*2 { bad = 1000000 }  i := 0  while i < len(b) { want := 0  if i % 2 == 0 { want = 1 }  if byte_at(b, i) != want { bad = bad + 1 }  i = i + 1 }  return }`

func TestChanBytesSurvivesSenderExit(t *testing.T) {
	main := `func main() { ch := make(chan bytes)  go worker(ch, 4096)  b := <-ch  sleep(50)  c := churn()  println(str(check(b, 4096)) + " " + str(len(c))) }`
	out, _ := buildRun(t, chanBytesWorker, chanBytesChurn, chanBytesCheck, main)
	if strings.TrimSpace(out) != "0 4000" {
		t.Fatalf("bare bytes over channel: got %q, want \"0 4000\" (0 bad bytes)", out)
	}
}

func TestChanStructWithBytesSurvivesSenderExit(t *testing.T) {
	decl := `type Res struct { idx int  data bytes  name string }`
	worker := `func worker2(ch, n) { parts := []string{}  i := 0  while i < n { parts = append(parts, "0100")  i = i + 1 }  ch <- Res{idx: 7, data: from_hex(join(parts, "")), name: "chunk-" + str(n)} }`
	main := `func main() { ch := make(chan Res)  go worker2(ch, 2048)  r := <-ch  sleep(50)  c := churn()  println(str(check(r.data, 2048)) + " " + str(r.idx) + " " + r.name + " " + str(len(c))) }`
	out := buildRunResetProg(t, decl, worker, chanBytesChurn, chanBytesCheck, main)
	if strings.TrimSpace(out) != "0 7 chunk-2048 4000" {
		t.Fatalf("struct with bytes over channel: got %q, want \"0 7 chunk-2048 4000\"", out)
	}
}

func TestGoArgBytesSurvivesCallerChurn(t *testing.T) {
	// the same offset list feeds mfl_go's argument freezing: a bytes argument must be
	// copied into the child's arena, not shared with the parent's.
	worker := `func consumer(b, n, done) { sleep(30)  done <- check(b, n) }`
	main := `func main() { done := make(chan int)  parts := []string{}  i := 0  while i < 1024 { parts = append(parts, "0100")  i = i + 1 }  b := from_hex(join(parts, ""))  go consumer(b, 1024, done)  arena_reset()  c := churn()  println(str(<-done) + " " + str(len(c))) }`
	out, _ := buildRun(t, worker, chanBytesChurn, chanBytesCheck, main)
	if strings.TrimSpace(out) != "0 4000" {
		t.Fatalf("bytes as go argument: got %q, want \"0 4000\"", out)
	}
}
