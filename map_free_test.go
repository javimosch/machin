package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// #667: a transient map made on the MAIN goroutine's arena takes the malloc
// path — a long-lived map has to survive arena_reset, and nothing
// distinguishes the two at make() — so it was never freed. A single-actor
// server that cannot put its request loop in a goroutine or an arena { } block
// (its engine is not goroutine-safe) had no way to reclaim a per-request map:
// 200k of them cost 104 MB, and arena_reset() did not touch them.
//
// The measurement is deliberately RELATIVE: build the same workload twice,
// once calling map_free and once not, and compare peak RSS. An absolute
// threshold would pass trivially if the measurement broke, which is exactly
// what happened to the first version of this test — it read /proc/self/status
// from MFL, where read_file returns 0 bytes because procfs reports st_size 0,
// and so compared 0 against 0 while map_free was disabled.

// peakRSSkB builds src, runs it, and returns the peak resident set the kernel
// charged it (ru_maxrss, kB on Linux).
func peakRSSkB(t *testing.T, src string) int64 {
	t.Helper()
	prog, err := ParseProgram([]string{normalize(src)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-mapfree-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(bin.Name())
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		t.Skip("no rusage on this platform")
	}
	return int64(ru.Maxrss)
}

const mapChurn = `func main() {
	i := 0
	while i < 200000 {
		m := make(map[string]string)
		m["host"] = "example.org"
		m["accept"] = "*/*"
		m["user-agent"] = "probe"
		%s
		i = i + 1
	}
}`

func TestMapFreeReclaimsMainArenaMaps(t *testing.T) {
	// ru_maxrss for a child includes what it inherited at fork, and the test
	// binary's own RSS grows as the suite runs — so the floor was ~10 MB when
	// this test ran alone and ~19 MB inside the full suite, which sank a
	// ratio-of-totals assertion that had passed in isolation. Measure an empty
	// program under the same conditions and compare the WORK each one did.
	floor := peakRSSkB(t, `func main() { }`)
	leaked := peakRSSkB(t, strings.Replace(mapChurn, "%s", "", 1))
	freed := peakRSSkB(t, strings.Replace(mapChurn, "%s", "map_free(m)", 1))
	leakedWork, freedWork := leaked-floor, freed-floor
	t.Logf("peak RSS: floor %d kB, %d kB without map_free, %d kB with (work: %d vs %d)",
		floor, leaked, freed, leakedWork, freedWork)
	if leakedWork < 20000 {
		t.Skipf("workload only grew %d kB above the %d kB floor — nothing to measure here", leakedWork, floor)
	}
	// ~100 MB of work without, a rounding error with. Assert an order of
	// magnitude rather than a number, so allocator differences do not flake.
	if freedWork*8 > leakedWork {
		t.Errorf("map_free did not reclaim: %d kB of growth with vs %d kB without", freedWork, leakedWork)
	}
}

// An arena-allocated map is owned by its arena. Freeing it here would double
// free at teardown, so map_free declines. The caller often cannot tell which
// path make() took, which is why this is a no-op rather than an error.
func TestMapFreeIsANoOpOnArenaMaps(t *testing.T) {
	src := `func main() {
	i := 0
	while i < 20000 {
		arena {
			m := make(map[string]string)
			m["a"] = "1"
			map_free(m)
		}
		i = i + 1
	}
	println("ok")
}`
	prog, err := ParseProgram([]string{normalize(src)})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bin, _ := os.CreateTemp("", "mfl-mapfree-arena-*")
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("arena-owned map_free crashed: %v", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("unexpected output %q", out)
	}
}

// The signature is enforced at compile time: a non-map argument and a wrong
// arity both have to be refused, or map_free becomes a way to free something
// that was never a map.
func TestMapFreeRejectsBadArguments(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"non-map", `func main() { s := "x"  map_free(s) }`, "map vs string"},
		{"two args", `func main() { m := make(map[string]string)  map_free(m, 1) }`, "1 arg (map)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := ParseProgram([]string{normalize(tc.src)})
			if err == nil {
				bin, _ := os.CreateTemp("", "mfl-mapfree-bad-*")
				bin.Close()
				defer os.Remove(bin.Name())
				err = BuildBinary(prog, bin.Name(), false)
			}
			if err == nil {
				t.Fatalf("expected a compile error mentioning %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
