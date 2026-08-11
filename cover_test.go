package main

import (
	"testing"
)

// `machin test --cover` — function-entry coverage (#589, #236 stage B1).
//
// The measurement is deliberately narrow: a function was ENTERED at least once.
// These tests pin the two properties that make it honest rather than flattering
// — an unreached function must lower the number, and "compiled" must not be
// mistaken for "covered".

// fileCov returns one file's slice of the report, matched on the EXACT path.
// Suffix matching is a trap here: "framework/test.src" ends with "t.src", so a
// suffix match silently returns the framework's coverage instead of the file
// under test.
func fileCov(t *testing.T, c *Coverage, path string) FileCoverage {
	t.Helper()
	for _, f := range c.Files {
		if f.File == path {
			return f
		}
	}
	t.Fatalf("no coverage entry for %q in %+v", path, c.Files)
	return FileCoverage{}
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// THE TRAP THIS EXISTS FOR: machin only instantiates functions something calls,
// so a coverage tool whose denominator came from emitted code would report 100%
// while silently omitting every function no test reaches. The denominator comes
// from the parsed AST instead, so an uncalled function is a MISS.
func TestCoverUncalledFunctionCountsAsMiss(t *testing.T) {
	dir := t.TempDir()
	lib := writeSrc(t, dir, "lib.src", `func used(x) (r) {
    r = x + 1
}

func never_called(x) (r) {
    r = x - 1
}`)
	tst := writeSrc(t, dir, "lib_test.src", `func main() {
    assert_eq_int(used(1), 2, "used")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{lib, tst}, true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Coverage == nil {
		t.Fatal("--cover produced no coverage report")
	}
	fc := fileCov(t, res.Coverage, lib)
	if fc.Total != 2 || fc.Covered != 1 {
		t.Fatalf("want 1/2 for lib.src, got %d/%d", fc.Covered, fc.Total)
	}
	if !hasName(fc.Uncovered, "never_called") {
		t.Fatalf("never_called should be listed uncovered, got %v", fc.Uncovered)
	}
	if hasName(fc.Uncovered, "used") {
		t.Fatalf("used was called and must not be uncovered: %v", fc.Uncovered)
	}
}

// A function called only from a branch that never executes IS compiled — it is
// instantiated and emitted. It must still report as uncovered, because the metric
// is entry, not instantiation. This is the difference between this number and
// "did the compiler emit it".
func TestCoverCompiledButNotEnteredIsUncovered(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func entered(x) (r) {
    r = x
}

func not_entered(x) (r) {
    r = x + 100
}

func main() {
    a := entered(1)
    if a == 999 {
        println(not_entered(a))
    }
    assert_eq_int(a, 1, "entered")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{f}, true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	fc := fileCov(t, res.Coverage, f)
	if !hasName(fc.Uncovered, "not_entered") {
		t.Fatalf("not_entered is compiled but never entered — must be uncovered, got %v", fc.Uncovered)
	}
	if hasName(fc.Uncovered, "entered") {
		t.Fatalf("entered ran and must be covered, got %v", fc.Uncovered)
	}
}

// A generic is monomorphized into one C function per type, but the author wrote
// ONE function. Coverage is keyed by source name so the instances collapse.
func TestCoverGenericInstancesCollapseToOneFunction(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func identity(x) (r) {
    r = x
}

func main() {
    a := identity(1)
    b := identity("s")
    assert_eq_int(a, 1, "int")
    assert_eq_str(b, "s", "str")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{f}, true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	fc := fileCov(t, res.Coverage, f)
	// identity + main, counted once each despite two instantiations of identity.
	if fc.Total != 2 || fc.Covered != 2 {
		t.Fatalf("want 2/2 (identity counted once), got %d/%d uncovered=%v", fc.Covered, fc.Total, fc.Uncovered)
	}
}

// --cover changes instrumentation only. If it moved the tally it would be
// changing the thing it claims to measure.
func TestCoverDoesNotChangeTheTally(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() {
    assert(1 + 1 == 2, "addition")
    assert_eq_int(3, 3, "int eq")
    test_summary()
}`)
	plain, _, err := runMFLTests([]string{f}, false)
	if err != nil {
		t.Fatalf("plain: %v", err)
	}
	covered, _, err := runMFLTests([]string{f}, true)
	if err != nil {
		t.Fatalf("cover: %v", err)
	}
	if plain.OK != covered.OK || plain.Passed != covered.Passed || plain.Failed != covered.Failed {
		t.Fatalf("--cover changed the tally: %+v vs %+v", plain, covered)
	}
	if plain.Coverage != nil {
		t.Fatal("coverage reported without --cover")
	}
	if covered.Coverage == nil {
		t.Fatal("no coverage reported with --cover")
	}
}

// The report must say what it measured. A number that cannot name its own
// granularity invites being quoted as statement or branch coverage.
func TestCoverStatesItsKindAndPercent(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func a(x) (r) {
    r = x
}

func b(x) (r) {
    r = x
}

func main() {
    assert_eq_int(a(1), 1, "a")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{f}, true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	c := res.Coverage
	if c.Kind != "function" {
		t.Fatalf("kind must be stated as %q, got %q", "function", c.Kind)
	}
	fc := fileCov(t, res.Coverage, f)
	// a and main run, b does not.
	if fc.Covered != 2 || fc.Total != 3 {
		t.Fatalf("want 2/3, got %d/%d uncovered=%v", fc.Covered, fc.Total, fc.Uncovered)
	}
	if c.Total < fc.Total || c.Covered < fc.Covered {
		t.Fatalf("totals must aggregate every file: %+v", c)
	}
	want := float64(c.Covered) / float64(c.Total) * 100
	if c.Pct < want-0.001 || c.Pct > want+0.001 {
		t.Fatalf("pct %v inconsistent with %d/%d", c.Pct, c.Covered, c.Total)
	}
}

// Statement coverage (#589 stage B2) exists because function coverage can read
// 100% while whole branches go unexercised. This is that case: classify() is
// called, so it is "covered" at function level, but two of its branches never
// run — and only the statement block says so.
func TestCoverStatementsCatchUnexecutedBranch(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "branch.src", `func classify(n) (r) {
    r = "zero"
    if n > 0 {
        r = "positive"
        if n > 100 {
            r = "big"
        }
    }
    if n < 0 {
        r = "negative"
    }
}

func main() {
    assert_eq_str(classify(5), "positive", "positive")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{f}, true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	fc := fileCov(t, res.Coverage, f)
	if fc.Covered != fc.Total {
		t.Fatalf("classify and main both ran; function coverage should be full, got %d/%d", fc.Covered, fc.Total)
	}
	sc := res.Coverage.Statements
	if sc == nil {
		t.Fatal("--cover produced no statement block")
	}
	if sc.Kind != "statement" {
		t.Fatalf("statement block must name its kind, got %q", sc.Kind)
	}
	if !hasName(sc.Uncovered, "classify") {
		t.Fatalf("classify has two unexecuted branches and must be listed, got %v", sc.Uncovered)
	}
	if sc.Covered >= sc.Total {
		t.Fatalf("unexecuted branches must lower statement coverage: %d/%d", sc.Covered, sc.Total)
	}
	if sc.Total <= res.Coverage.Total {
		t.Fatalf("statements should outnumber functions, got %d statements vs %d functions", sc.Total, res.Coverage.Total)
	}
}

// A function whose every statement runs must NOT be listed as having unexecuted
// statements — otherwise the list is noise and gets ignored.
func TestCoverFullyExercisedFunctionNotListed(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "full.src", `func double(n) (r) {
    r = n * 2
}

func main() {
    assert_eq_int(double(4), 8, "double")
    test_summary()
}`)
	res, _, err := runMFLTests([]string{f}, true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	sc := res.Coverage.Statements
	if sc == nil {
		t.Fatal("no statement block")
	}
	if hasName(sc.Uncovered, "double") {
		t.Fatalf("double's only statement ran; it must not be listed: %v", sc.Uncovered)
	}
}

// A fully covered file must still emit an empty [] for Uncovered, not null.
// Null is a shape bug for consumers that expect an array; `null` also makes it
// impossible to tell "no uncovered functions" from "this field is missing".
func TestCoverUncoveredListsAreEmptyNotNull(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "t.src", `func main() {
    test_summary()
}`)
	res, _, err := runMFLTests([]string{f}, true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Coverage == nil {
		t.Fatal("no coverage report")
	}
	for _, fc := range res.Coverage.Files {
		if fc.Uncovered == nil {
			t.Fatalf("file %q: Uncovered is nil, want []", fc.File)
		}
	}
	if sc := res.Coverage.Statements; sc != nil && sc.Uncovered == nil {
		t.Fatal("statement coverage Uncovered is nil, want []")
	}
}
