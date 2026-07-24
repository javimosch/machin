package main

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// #529: arena_reset() frees the arena chain, but plain free() leaves the pages
// on glibc's heap free-list, so RSS stayed pinned at the peak. arena_reset() now
// calls malloc_trim(0) (glibc) to return those pages to the OS. This test churns
// to a multi-hundred-MB high-water mark, then asserts RSS collapses after a
// reset. glibc/Linux only — malloc_trim is a no-op elsewhere (and the RSS probe
// reads /proc), so the test skips on other platforms.
func TestArenaResetReturnsRSSToOS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RSS-return check is glibc/Linux-specific (malloc_trim)")
	}
	rssFn := `func rss_kb() (r) {
	code, out, err := exec("grep VmRSS /proc/$PPID/status | grep -o '[0-9]*'")
	r = parse_int(trim(out))
}`
	churnFn := `func churn(n) (acc) {
	acc = 0
	i := 0
	while i < n {
		s := "row-" + str(i) + "-payload-" + str(i * 7) + "-" + str(i * 13)
		acc = acc + len(s)
		i = i + 1
	}
}`
	mainFn := `func main() {
	a := churn(500000)
	peak := rss_kb()
	arena_reset()
	after := rss_kb()
	println(str(peak) + " " + str(after) + " " + str(a))
}`

	p, err := ParseProgram([]string{normalize(rssFn), normalize(churnFn), normalize(mainFn)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-trim-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(p, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		t.Fatalf("unexpected output %q", out)
	}
	peak, _ := strconv.Atoi(fields[0])
	after, _ := strconv.Atoi(fields[1])
	if peak == 0 || after == 0 {
		t.Skipf("RSS probe unavailable in this environment (peak=%d after=%d)", peak, after)
	}
	// The churn allocates well over 100 MB into the arena; if the fix works,
	// after-reset RSS collapses to a small fraction of the peak.
	if peak < 100_000 {
		t.Fatalf("churn did not build the expected high-water RSS (peak=%d KB)", peak)
	}
	if after > peak/4 {
		t.Fatalf("arena_reset did not return memory to the OS: peak=%d KB, after=%d KB (want after <= peak/4)", peak, after)
	}
}
