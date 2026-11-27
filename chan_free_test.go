package main

// #724: channels are malloc'd and were never freed — not even by close() — so a
// program that keeps making channels grew without bound (~156 bytes each). The
// goroutine half of the issue was fixed by #726; chan_free is the channel half:
// an explicit, unchecked hand-back in the map_free (#667) / free() mold.

import (
	"os"
	"os/exec"
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

// #740: under --safe, chan_free misuse is a panic, not UB — use-after-free,
// double-free, close-after-free, and freeing under a blocked receiver all fail
// loudly instead of crashing or hanging. (Unsafe builds keep the unchecked
// free(); the self-hosted compiler never emits --safe code, so it always gets
// the unchecked mfl_chan_free.)
func TestChanFreeSafePanicsOnMisuse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"double free", `func main() { c := make(chan int)  chan_free(c)  chan_free(c) }`, "already freed"},
		{"send after free", `func main() { c := make(chan int)  chan_free(c)  c <- 1 }`, "freed by chan_free"},
		{"recv after free", `func main() { c := make(chan int)  chan_free(c)  x := <-c  println(x) }`, "freed by chan_free"},
		{"close after free", `func main() { c := make(chan int)  chan_free(c)  close(c) }`, "freed by chan_free"},
		{"free under blocked receiver", "func waiter(c, done) { x := <-c  println(x)  done <- 1 }\nfunc main() { c := make(chan int)  done := make(chan int)  go waiter(c, done)  sleep(50)  chan_free(c)  d := <-done  println(d) }", "freed by chan_free"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lines []string
			for _, ln := range strings.Split(tc.src, "\n") {
				lines = append(lines, normalize(ln))
			}
			prog, err := ParseProgram(lines)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			bin, _ := os.CreateTemp("", "mfl-chanfree-safe-*")
			bin.Close()
			defer os.Remove(bin.Name())
			if err := BuildBinary(prog, bin.Name(), true); err != nil {
				t.Fatalf("build: %v", err)
			}
			out, err := exec.Command(bin.Name()).CombinedOutput()
			if err == nil {
				t.Fatalf("expected a panic, program exited 0: %s", out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("output %q does not contain %q", out, tc.want)
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
