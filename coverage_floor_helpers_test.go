package main

import "testing"

// TestCoverageOutcome pins the hardening decision table: a failed test with a valid profile is a
// NOTE (coverage still measured), a missing profile is FATAL (build error / crash).
func TestCoverageOutcome(t *testing.T) {
	cases := []struct {
		haveProfile, runFailed bool
		fatal, note            bool
	}{
		{true, false, false, false}, // clean run
		{true, true, false, true},   // a test failed but coverage is valid → note, NOT fatal (the fix)
		{false, true, true, false},  // no profile + run failed → build error/crash → fatal
		{false, false, true, false}, // no profile, no run error → unparseable profile → fatal
	}
	for _, c := range cases {
		fatal, note := coverageOutcome(c.haveProfile, c.runFailed)
		if fatal != c.fatal || note != c.note {
			t.Errorf("coverageOutcome(%v,%v) = (fatal=%v,note=%v), want (fatal=%v,note=%v)",
				c.haveProfile, c.runFailed, fatal, note, c.fatal, c.note)
		}
	}
}

// TestFixtureStatus + TestTailLines cover the small reporting helpers.
func TestFixtureStatus(t *testing.T) {
	if fixtureStatus(88.0, 87.0) != "intact" {
		t.Error("above floor should be intact")
	}
	if fixtureStatus(86.0, 87.0) != "REGRESSED" {
		t.Error("below floor should be REGRESSED")
	}
	if fixtureStatus(87.0, 87.0) != "intact" {
		t.Error("exactly at floor should be intact")
	}
}

func TestTailLines(t *testing.T) {
	if got := tailLines("a\nb\nc\nd", 2); got != "c\nd" {
		t.Errorf("tailLines = %q, want %q", got, "c\nd")
	}
	if got := tailLines("only", 5); got != "only" {
		t.Errorf("tailLines fewer-than-n = %q", got)
	}
}
