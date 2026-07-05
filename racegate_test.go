package main

import (
	"strings"
	"testing"
)

// raceGate had no direct test — only exercised indirectly through cmdRun/cmdBuild
// via --race-safe. Cover its three outcomes: clean program, type error (defers to
// the normal compile path), and a genuine race (formatted error).
func TestRaceGate_Clean(t *testing.T) {
	prog, err := ParseProgram([]string{"func main(){x:=1 print(x)}"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := raceGate(prog); err != nil {
		t.Fatalf("raceGate on clean program: got %v, want nil", err)
	}
}

func TestRaceGate_TypeError(t *testing.T) {
	prog, err := ParseProgram([]string{"func main(){x:=undefined_fn()}"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := raceGate(prog); err != nil {
		t.Fatalf("raceGate should defer type errors to the normal compile path, got %v", err)
	}
}

func TestRaceGate_RaceFound(t *testing.T) {
	prog, err := ParseProgram([]string{
		"func worker(xs, id){xs[id]=id*2}",
		"func main(){data:=[]int{0,0,0,0} go worker(data,0) go worker(data,1)}",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = raceGate(prog)
	if err == nil {
		t.Fatal("raceGate should report the write/write race, got nil")
	}
	const want = "data race(s) detected (--race-safe): 1"
	if got := err.Error(); !strings.Contains(got, want) {
		t.Fatalf("raceGate error = %q, want to contain %q", got, want)
	}
}
