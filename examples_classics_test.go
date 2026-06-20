package main

import "testing"

// runExample loads a .mfl example file via the real loader (base64 lines ->
// FuncDecls), compiles it to native via cc, runs it, and returns stdout.
func runExample(t *testing.T, path string) string {
	t.Helper()
	fns, err := loadMFL(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	out, err := RunCaptured(fns)
	if err != nil {
		t.Fatalf("run %s: %v", path, err)
	}
	return out
}

func TestArmstrongExample(t *testing.T) {
	// The four three-digit narcissistic numbers.
	if got := runExample(t, "examples/complex/armstrong.mfl"); got != "153\n370\n371\n407\n" {
		t.Fatalf("got %q", got)
	}
}

func TestNthPrimeExample(t *testing.T) {
	if got := runExample(t, "examples/complex/nth_prime.mfl"); got != "2 11 29\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPowerTableExample(t *testing.T) {
	want := "1 2 1\n2 4 4\n3 8 9\n4 16 16\n5 32 25\n"
	if got := runExample(t, "examples/complex/power_table.mfl"); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestRunningSumExample(t *testing.T) {
	// Prefix sums of {3,1,4,1,5}; exercises slices, len, and for loops.
	if got := runExample(t, "examples/complex/running_sum.mfl"); got != "3\n4\n8\n9\n14\n" {
		t.Fatalf("got %q", got)
	}
}

func TestBaseConvertExample(t *testing.T) {
	// 13 in base 2, 255 in base 8, 42 in base 5 (bases <= 10 so digits map
	// directly); exercises recursive string building via str().
	if got := runExample(t, "examples/complex/base_convert.mfl"); got != "1101\n377\n132\n" {
		t.Fatalf("got %q", got)
	}
}
