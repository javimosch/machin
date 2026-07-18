package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// typesCoverageFloor is the locked-in floor for package statement
// coverage. The Phase 1-6 coverage push ran package total to ~89.1%
// (the 89.1%-cited measurement was made WITHOUT this regression-guard
// test file present). Adding coverage_floor_test.go itself adds ~70
// statements to the package denominator (mostly uncovered in the
// subprocess reentry-skip path), which costs ~1.2pp on package total
// — landing the actual measured floor at 87.80%. We lock at 87.0%
// for ~0.8pp headroom, comfortably catching any single-phase-1-6
// fixture deletion (which individually drops coverage by 0.5-1.4pp)
// while tolerating small measurement variance.
//
// The test catches an accidental regression by re-running the suite
// with -coverprofile and asserting this threshold. Bump deliberately
// when new fixtures raise coverage; do not lower.
//
// Overridable via the TYPES_COVERAGE_FLOOR env var (the Makefile
// `cov-floor` target reads it; CI can set it for stress runs).
const typesCoverageFloor = 87.0

// coverageReentryEnv breaks the recursion that would otherwise happen
// if the subprocess this test spawns also ran this test. Set self-
// recursively only.
const coverageReentryEnv = "COVERAGE_FLOOR_REENTRY"

// coverageSkip is a -skip regex for the coverage subprocess: slow runtime
// INTEGRATION tests that do not drive the front-end (Phase 1-6) coverage this
// floor guards, so excluding them from the measurement costs ~0.1pp of package
// total (measured) while cutting the subprocess ~40% (73s -> 45s). Two of them
// (the ru_maxrss arena-ratio tests) are additionally env-dependent and can flake
// under the parallel, coverage-instrumented subprocess; dropping them here removes
// that flake at its source. These tests still run — and gate pass/fail — in the
// normal `go test .` invocation; only this coverage-MEASUREMENT reentry skips them.
const coverageSkip = "TestStaticBuildBundlesSqlite|" +
	"TestDeadlockStrictIO|" +
	"TestReadBytesLoopReassemblesLargePayload|" +
	"TestScopedArenaReclaimsInlineAllocations|" +
	"TestScopedArenaBoundsMemory"

// TestTypesCoverageFloor is the regression guard for the Phase 1-6
// coverage push. It spawns `go test -short -skip=<slow-integration>
// -coverprofile=X .` as a subprocess so the coverage profile includes
// all of Phase 1-6 + whatever else is in the suite, parses X for the
// package-total statement coverage via `go tool cover -func`, and
// asserts that ≥ typesCoverageFloor. The -skip drops a handful of slow
// runtime integration tests (see coverageSkip) that don't drive this
// floor — ~40% faster subprocess — and coverage is read from the
// profile even if a test fails (see coverageOutcome), so a flaky
// integration test can't turn the floor red.
//
// Fires inside the standard `go test ./...` invocation (and so inside
// the existing CI step) — no separate CI step required. Skips under
// `go test -short ./...` so local devs iterating fast aren't blocked
// by a ~30s subprocess run; the Makefile `cov-floor` target invokes
// the test directly with `-count=1 -run` so it does run.
func TestTypesCoverageFloor(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess coverage check skipped under -short")
	}
	if os.Getenv(coverageReentryEnv) == "1" {
		t.Skip("skipping in subprocess reentry")
	}

	floor := typesCoverageFloor
	if v := os.Getenv("TYPES_COVERAGE_FLOOR"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("TYPES_COVERAGE_FLOOR=%q: %v", v, err)
		}
		floor = f
	}

	profilePath := filepath.Join(t.TempDir(), "coverage.out")
	cmd := exec.Command("go", "test",
		"-count=1", "-short",
		"-skip="+coverageSkip,
		"-coverprofile="+profilePath,
		".")
	cmd.Env = append(os.Environ(), coverageReentryEnv+"=1")
	raw, runErr := cmd.CombinedOutput()

	// Measure coverage from the profile, which `go test` writes even when a test
	// fails. This test's job is to guard the coverage NUMBER, not to re-adjudicate
	// the suite's pass/fail — the outer `go test` run already does that. So a flaky
	// integration test (e.g. an env-dependent ru_maxrss ratio) must not turn the
	// coverage floor red. Only fail here if the profile is unusable, which means the
	// subprocess never got far enough to measure coverage (a build error or a panic
	// that crashed the binary before the profile was written).
	got, covErr := coverPkgTotal(profilePath)
	fatal, note := coverageOutcome(covErr == nil, runErr != nil)
	if fatal {
		if runErr != nil {
			t.Fatalf("coverage subprocess produced no usable profile (build error or crash, not a coverage regression): %v\n%s",
				runErr, tailLines(string(raw), 20))
		}
		t.Fatalf("coverPkgTotal: %v\nsubprocess tail:\n%s", covErr, tailLines(string(raw), 15))
	}
	if note {
		// The profile is valid, so coverage is measurable; a test failed but that is
		// the outer run's concern, not the floor guard's. Surface it as a note.
		t.Logf("note: a test failed in the coverage subprocess (coverage still measured from the written profile; the main `go test` run is the pass/fail gate):\n%s",
			tailLines(string(raw), 8))
	}
	t.Logf("package total: %.2f%% (floor %.2f%%) — Phase 1-6 fixtures %s",
		got, floor, fixtureStatus(got, floor))

	if got+1e-9 < floor {
		t.Errorf("package total %.2f%% < floor %.2f%%\n"+
			"    pass = %.2f%%, gap = %.2f pp\n"+
			"    Phase 1-6 fixtures that drive this number:\n"+
			"      coverage_phase1_test.go (Phase 1+2)\n"+
			"      types_builtins_test.go    parser_errors_test.go\n"+
			"      build_args_test.go        types_stmts_test.go  (Phase 3)\n"+
			"      types_multiassign_test.go                  (Phase 4)\n"+
			"      types_stmt_branches_test.go                (Phase 5)\n"+
			"      types_phase6_deep_test.go                  (Phase 6)\n"+
			"    A deletion or weakening of one of these will drop coverage; "+
			"restore / strengthen the fixture (or, deliberately, bump the floor).",
			got, floor, got, floor-got)
	}
}

// coverageOutcome is the decision table for the coverage subprocess. `haveProfile` is whether a
// parseable coverage profile was produced; `runFailed` is whether the subprocess `go test`
// exited non-zero. A missing profile means the subprocess never got far enough to measure
// coverage (a build error or a crash before the profile flush) → fatal. A valid profile with a
// run failure means a test failed but coverage is still measurable → not fatal (the outer
// `go test` is the pass/fail gate); surface it as a note so a flaky integration test can't turn
// the coverage floor red.
func coverageOutcome(haveProfile bool, runFailed bool) (fatal, note bool) {
	if !haveProfile {
		return true, false
	}
	return false, runFailed
}

// coverPkgTotal parses the canonical `total: (statements) NN.N%` line
// from `go tool cover -func`. We resolve via subprocess rather than
// re-implement the format so we track Go's own definition.
func coverPkgTotal(profilePath string) (float64, error) {
	cmd := exec.Command("go", "tool", "cover", "-func="+profilePath)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "total:") {
			continue
		}
		// Last parseable %-suffixed number on the line is the value.
		fields := strings.Fields(line)
		for i := len(fields) - 1; i >= 0; i-- {
			tok := strings.TrimSuffix(fields[i], "%")
			if pct, err := strconv.ParseFloat(tok, 64); err == nil {
				return pct, nil
			}
		}
	}
	return 0, fmt.Errorf("no total: line in `go tool cover -func` for %s", profilePath)
}

func fixtureStatus(got, floor float64) string {
	if got+1e-9 >= floor {
		return "intact"
	}
	return "REGRESSED"
}

func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
