package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Adversarial alias suite, run under AddressSanitizer (#578 step 2).
//
// THIS IS THE NET, BUILT BEFORE THE THING IT CATCHES. Every program here keeps a
// LIVE alias across an `append`. Today they all pass trivially: `mfl_realloc`
// allocates a fresh block and abandons the old one, so every alias stays valid.
//
// They earn their keep when step 3 teaches `append` to grow in place for slices
// `machin alias` proves unaliased. If that analysis ever wrongly qualifies one of
// these shapes, the alias becomes a dangling read — and ASan turns that from a
// silent wrong answer into a loud failure with a stack trace. Verified against a
// deliberately unsound prototype:
//
//	==921117==ERROR: AddressSanitizer: heap-use-after-free on address 0x504000000020
//	READ of size 8 at 0x504000000020 thread T0
//	    #0 in mfl_main
//
// Each program also asserts the VALUE it reads, so a build with no sanitizer
// still catches the corruption if it happens to land inside a live allocation.

// asanCC writes a cc wrapper that adds -fsanitize=address, and reports whether
// the toolchain can actually use it (libasan is not installed everywhere).
func asanCC(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cc := filepath.Join(dir, "cc-asan")
	if err := os.WriteFile(cc, []byte("#!/bin/sh\nexec cc -fsanitize=address -g \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(probe, []byte("int main(void){return 0;}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(cc, probe, "-o", filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Skipf("AddressSanitizer unavailable on this toolchain: %v\n%s", err, out)
	}
	return cc
}

// The alias must survive enough appends to force several reallocations; a single
// append may fit in spare capacity and move nothing.
const aliasGrowLoop = `
    i := 0
    while i < 5000 { a = append(a, i)  i = i + 1 }`

func TestAliasAdversarialUnderASan(t *testing.T) {
	if testing.Short() {
		t.Skip("ASan builds are slow")
	}
	cc := asanCC(t)

	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// The plain case: another local holds the same array.
			name: "local_copy_live_across_appends",
			src: `func main() {
    a := []int{11, 22, 33}
    b := a` + aliasGrowLoop + `
    println(str(b[0]) + "," + str(b[1]) + "," + str(b[2]) + "," + str(len(b)))
}`,
			want: "11,22,33,3",
		},
		{
			// A struct field holds it — the alias outlives the statement that made it.
			name: "struct_field_live_across_appends",
			src: `type Box struct { xs []int }

func main() {
    a := []int{44, 55}
    box := Box{xs: a}` + aliasGrowLoop + `
    println(str(box.xs[0]) + "," + str(box.xs[1]) + "," + str(len(box.xs)))
}`,
			want: "44,55,2",
		},
		{
			// A closure captures it BY REFERENCE and is called after the growth.
			name: "closure_capture_live_across_appends",
			src: `func main() {
    a := []int{66, 77}
    f := func() { return a[0] + a[1] }` + aliasGrowLoop + `
    println(f())
}`,
			want: "143",
		},
		{
			// A package global keeps the alias alive for the whole program.
			name: "global_alias_live_across_appends",
			src: `var g = []int{}

func main() {
    a := []int{88, 99}
    g = a` + aliasGrowLoop + `
    println(str(g[0]) + "," + str(g[1]) + "," + str(len(g)))
}`,
			want: "88,99,2",
		},
		{
			// Alias and append in the SAME loop: the alias taken on one iteration is
			// live across the next iteration's append.
			name: "alias_and_append_in_same_loop",
			src: `func main() {
    a := []int{7}
    keep := []int{}
    i := 0
    while i < 2000 {
        keep = a
        a = append(a, i)
        i = i + 1
    }
    println(str(keep[0]) + "," + str(len(keep)))
}`,
			want: "7,2000",
		},
		{
			// A slice inside a slice of structs — the alias is reachable only through
			// two hops, which is exactly where a shallow analysis would lose track.
			name: "alias_held_in_a_slice_of_structs",
			src: `type Box struct { xs []int }

func main() {
    a := []int{123, 456}
    boxes := []Box{}
    boxes = append(boxes, Box{xs: a})` + aliasGrowLoop + `
    println(str(boxes[0].xs[0]) + "," + str(boxes[0].xs[1]) + "," + str(len(boxes[0].xs)))
}`,
			want: "123,456,2",
		},
		{
			// The SAFE shape, included on purpose: nothing aliases it during growth,
			// so step 3 should optimize this one — and it must still be correct.
			name: "safe_build_then_use",
			src: `func main() {
    a := []int{}` + aliasGrowLoop + `
    println(str(a[0]) + "," + str(a[4999]) + "," + str(len(a)))
}`,
			want: "0,4999,5000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "p.src")
			if err := os.WriteFile(src, []byte(tc.src), 0o644); err != nil {
				t.Fatal(err)
			}
			prog := progFromSrc(t, tc.src)
			bin := filepath.Join(dir, "p")

			// Build through the sanitizing cc. BuildBinary shells out to $CC.
			old, had := os.LookupEnv("CC")
			os.Setenv("CC", cc)
			err := BuildBinary(prog, bin, false)
			if had {
				os.Setenv("CC", old)
			} else {
				os.Unsetenv("CC")
			}
			if err != nil {
				t.Fatalf("build: %v", err)
			}

			cmd := exec.Command(bin)
			// LeakSanitizer off: machin's arena retains allocations by design and
			// frees them in bulk at exit, so "leaks" here are the allocation model,
			// not a bug. The failure this suite hunts is USE-AFTER-FREE, which
			// detect_leaks does not affect.
			cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0")
			out, runErr := cmd.CombinedOutput()
			got := strings.TrimSpace(string(out))
			if strings.Contains(got, "AddressSanitizer") {
				t.Fatalf("ASan reported a memory error — an alias was invalidated:\n%s", got)
			}
			if runErr != nil {
				t.Fatalf("run: %v\n%s", runErr, got)
			}
			if got != tc.want {
				t.Fatalf("alias read the wrong data\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}
