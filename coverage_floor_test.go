//go:build !cover_floor

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
// coverage, set after Phase 1-6 of the types.go coverage push
// (coverage_phase1_test.go, types_builtins_test.go, parser_errors_test.go,
// build_args_test.go, types_stmts_test.go, types_multiassign_test.go,
// types_stmt_branches_test.go, types_phase6_deep_test.go). The test
// catches an accidental regression by re-running the suite with
// -coverprofile and asserting this threshold. Bump deliberately when
// new fixtures raise coverage; do not lower.
//
// Overridable via the TYPES_COVERAGE_FLOOR env var (the Makefile
// `cov-floor` target reads it; CI can set it for stress runs).
const typesCoverageFloor = 88.5

// TestTypesCoverageFloor is the regression guard for the Phase 1-6
// coverage push. It spawns `go test -short -coverprofile=X
// -tags=cover_floor .` as a subprocess so the coverage profile includes
// all of Phase 1-6 + whatever else is in the suite, parses X for the
// package-total statement coverage via `go tool cover -func`, and
// asserts that ≥ typesCoverageFloor.
//
// Recursion is broken with a build tag. The subprocess passes
// `-tags cover_floor`, which excludes THIS file entirely from the
// subprocess's compile unit — so coverage_floor_test.go's own
// statements never enter the subprocess's package-coverage denominator.
// (An earlier version used an env-var recursion guard with t.Skip,
// which cost ~1.2pp on package total by inflating the subprocess
// denominator with the test's own body. The build-tag approach
// recovers that headroom.) Recovery lets the floor sit ~88.5% — much
// closer to the post-Phase-1-6 89.1% reference.
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

	floor := typesCoverageFloor
	if v := os.Getenv("TYPES_COVERAGE_FLOOR"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("TYPES_COVERAGE_FLOOR=%q: %v", v, err)
		}
		floor = f
	}

	profilePath := filepath.Join(t.TempDir(), "coverage.out")
	// `-tags cover_floor` excludes this file from the subprocess
	// build, so its statements don't enter the subprocess's coverage
	// denominator (no env-var skip path needed).
	cmd := exec.Command("go", "test", "-count=1", "-short",
		"-tags", "cover_floor",
		"-coverprofile="+profilePath,
		".")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess `go test` failed: %v\n%s", err, raw)
	}

	got, err := coverPkgTotal(profilePath)
	if err != nil {
		t.Fatalf("coverPkgTotal: %v\nsubprocess tail:\n%s",
			err, tailLines(string(raw), 15))
	}
	t.Logf("package total: %.2f%% (floor %.2f%%)", got, floor)

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

// coverPkgTotal parses the canonical `total: (statements) NN.N%` line
// from `go tool cover -func`. We resolve via subprocess rather than
// re-implementing the format so we track Go's own definition.
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

func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
