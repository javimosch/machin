package main

import "testing"

// runMFLFile loads an on-disk .mfl example through the real machine-first path
// (base64 → parse → compile to native via cc → run) and returns its stdout.
func runMFLFile(t *testing.T, path string) string {
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

// TestExtraExamples locks in the output of the example programs added in this
// branch so they cannot silently rot as the compiler evolves.
func TestExtraExamples(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"examples/complex/tribonacci.mfl", "0 0 1 1 2 4 7 13 24 44 81 149 274 \n"},
		{"examples/complex/insertion_sort.mfl", "1 2 3 4 5 6 7 8 9 \n"},
		{"examples/complex/popcount.mfl", "0 -> 0 bits set\n1 -> 1 bits set\n2 -> 1 bits set\n"},
	}
	for _, tc := range cases {
		got := runMFLFile(t, tc.path)
		if tc.path == "examples/complex/popcount.mfl" {
			// Only assert the prefix; full output is long.
			if len(got) < len(tc.want) || got[:len(tc.want)] != tc.want {
				t.Fatalf("%s: got %q, want prefix %q", tc.path, got, tc.want)
			}
			continue
		}
		if got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestHappyNumbers verifies the Floyd-cycle happy-number example reports the
// known happy numbers in 1..20.
func TestHappyNumbers(t *testing.T) {
	got := runMFLFile(t, "examples/complex/happy_number.mfl")
	want := "1 is happy\n7 is happy\n10 is happy\n13 is happy\n19 is happy\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
