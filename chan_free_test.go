package main

// #724: channels are malloc'd and were never freed — not even by close() — so a
// program that keeps making channels grew without bound (~156 bytes each). The
// goroutine half of the issue was fixed by #726; chan_free is the channel half:
// an explicit, unchecked hand-back in the map_free (#667) / free() mold.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const chanChurn = `func main() {
	i := 0
	while i < 200000 {
		c := make(chan int)
		%s
		i = i + 1
	}
}`

func TestChanFreeReclaimsChannels(t *testing.T) {
	// Same relative measurement as the map_free test (#667): an absolute RSS
	// threshold passes trivially if the measurement broke, so compare the WORK
	// each variant did above an empty-program floor.
	floor := peakRSSkB(t, `func main() { }`)
	leaked := peakRSSkB(t, strings.Replace(chanChurn, "%s", "", 1))
	freed := peakRSSkB(t, strings.Replace(chanChurn, "%s", "chan_free(c)", 1))
	leakedWork, freedWork := leaked-floor, freed-floor
	t.Logf("peak RSS: floor %d kB, %d kB without chan_free, %d kB with (work: %d vs %d)",
		floor, leaked, freed, leakedWork, freedWork)
	if leakedWork < 20000 {
		t.Skipf("workload only grew %d kB above the %d kB floor — nothing to measure here", leakedWork, floor)
	}
	// ~30 MB of channels without, a rounding error with. An order of magnitude,
	// not a number, so allocator differences do not flake.
	if freedWork*8 > leakedWork {
		t.Errorf("chan_free did not reclaim: %d kB of growth with vs %d kB without", freedWork, leakedWork)
	}
}

// Queued messages are freed with the channel — including the frozen string
// copies a string-element channel carries. A chan_free that only freed the
// struct would leak (or corrupt) every undelivered payload.
func TestChanFreeDiscardsQueuedMessages(t *testing.T) {
	src := `func main() {
	c := make(chan string)
	c <- "hello"
	c <- "world"
	s := make(chan int)
	s <- 1
	s <- 2
	close(c)
	chan_free(c)
	chan_free(s)
	println("ok")
}`
	prog, err := ParseProgram([]string{normalize(src)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, _ := os.CreateTemp("", "mfl-chanfree-*")
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("chan_free with queued messages crashed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("unexpected output %q", out)
	}
}

// #740: the misuse modes that can be caught without ownership tracking are
// caught — a double chan_free, a stale chan_free after the address would have
// been reused, a free under a blocked receiver or a spinning select, and a use
// while the freed header is still quarantined are all clean panics, not
// UB/crashes/hangs.
func TestChanFreePanicsOnMisuse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"double free", `func main() {
	c := make(chan int)
	chan_free(c)
	chan_free(c)
}`, "double free"},
		{"stale free after reuse", `func main() {
	a := make(chan int)
	chan_free(a)
	b := make(chan int)
	chan_free(a)
	b <- 7
}`, "not a live channel"},
		{"free under select", `func w(c, done) {
	select {
	case v := <-c:
		println(v)
	}
	done <- 1
}
func main() {
	c := make(chan int)
	done := make(chan int)
	go w(c, done)
	sleep(50)
	chan_free(c)
	x := <-done
	println(x)
}`, "still selecting"},
		{"blocked receiver", `func worker(c, started) {
	started <- 1
	x := <-c
	println(x)
}
func main() {
	c := make(chan int)
	started := make(chan int)
	go worker(c, started)
	<-started
	sleep(50)
	chan_free(c)
}`, "blocked receiving"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := wallsCompose(t, tc.src)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			bin, _ := os.CreateTemp("", "mfl-chanfree-misuse-*")
			bin.Close()
			defer os.Remove(bin.Name())
			if err := BuildBinary(prog, bin.Name(), false); err != nil {
				t.Fatalf("build: %v", err)
			}
			cmd := exec.Command(bin.Name())
			var so, se strings.Builder
			cmd.Stdout, cmd.Stderr = &so, &se
			err = cmd.Run()
			if err == nil {
				t.Fatalf("expected a panic, program exited 0: %q", so.String())
			}
			if !strings.Contains(se.String(), "panic: chan_free") || !strings.Contains(se.String(), tc.want) {
				t.Errorf("stderr %q does not report %q", se.String(), tc.want)
			}
		})
	}
}

// While a freed header sits in the bounded quarantine its address cannot be
// recycled, so it stays marked dead and a stale op panics cleanly instead of
// hitting freed memory — the normal-build half of what --safe does forever.
func TestChanFreeQuarantineCatchesStaleUse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"send after free", "func main() {\n\tc := make(chan int)\n\tchan_free(c)\n\tc <- 1\n}", "send on a channel freed"},
		{"receive after free", "func main() {\n\tc := make(chan int)\n\tchan_free(c)\n\tx := <-c\n\tprintln(x)\n}", "receive on a channel freed"},
		{"close after free", "func main() {\n\tc := make(chan int)\n\tchan_free(c)\n\tclose(c)\n}", "close on a channel freed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := wallsCompose(t, tc.src)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			bin := filepath.Join(t.TempDir(), "p")
			if err := BuildBinary(prog, bin, false); err != nil {
				t.Fatalf("build: %v", err)
			}
			out, err := exec.Command("timeout", "10", bin).CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Errorf("%v: output %q does not report %q", err, out, tc.want)
			}
		})
	}
}

// The signature is enforced at compile time: a non-channel argument and a
// wrong arity both have to be refused, or chan_free becomes a way to free
// something that was never a channel.
func TestChanFreeRejectsBadArguments(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"non-chan", `func main() { s := "x"  chan_free(s) }`, ""},
		{"two args", `func main() { c := make(chan int)  chan_free(c, 1) }`, "1 arg (chan)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := ParseProgram([]string{normalize(tc.src)})
			if err == nil {
				bin, _ := os.CreateTemp("", "mfl-chanfree-bad-*")
				bin.Close()
				defer os.Remove(bin.Name())
				err = BuildBinary(prog, bin.Name(), false)
			}
			if err == nil {
				t.Fatalf("expected a compile error, got none")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// --safe keeps the freed header forever, which closes the gaps a normal build
// only bounds (#740): the stale-use window never ends, where the quarantine
// caps it at MFL_CHQUAR later frees.
func TestChanFreeSafeClosesTheGaps(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"stale double free", `func main() {
	a := make(chan int)
	chan_free(a)
	b := make(chan int)
	chan_free(a)
	b <- 7
}`, "double free"},
		{"free under select", `func w(c, done) {
	select {
	case v := <-c:
		println(v)
	}
	done <- 1
}
func main() {
	c := make(chan int)
	done := make(chan int)
	go w(c, done)
	sleep(50)
	chan_free(c)
	x := <-done
	println(x)
}`, "still selecting"},
		{"send after free", "func main() {\n\tc := make(chan int)\n\tchan_free(c)\n\tc <- 1\n}", "send on a channel freed"},
		{"receive after free", "func main() {\n\tc := make(chan int)\n\tchan_free(c)\n\tx := <-c\n\tprintln(x)\n}", "receive on a channel freed"},
		{"close after free", "func main() {\n\tc := make(chan int)\n\tchan_free(c)\n\tclose(c)\n}", "close on a channel freed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := wallsCompose(t, tc.src)
			if err != nil {
				t.Fatalf("compose: %v", err)
			}
			bin := filepath.Join(t.TempDir(), "p")
			if err := BuildBinary(prog, bin, true); err != nil {
				t.Fatalf("build: %v", err)
			}
			out, err := exec.Command("timeout", "10", bin).CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Errorf("%v: output %q does not report %q", err, out, tc.want)
			}
		})
	}
}
