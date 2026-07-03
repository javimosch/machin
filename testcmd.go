package main

// `machin test [--json] <src...>` — Stage A of #236 (native MFL test runner).
// Composes framework/test.src ahead of the given sources (same multi-file
// compose `machin encode` already does — so testing a framework module means
// passing it alongside its test file: `machin test framework/flags.src
// framework/tests/flags_test.src`), builds the result as one program, runs
// it, and reports the "TEST_SUMMARY passed=N failed=M" line framework/test.src's
// test_summary() prints. Sugar over the same compose->build->run path
// `machin encode`/`machin build` already use — a way to write and run
// framework/app tests in MFL, without the Go harness (RunCaptured). Not a new
// measurement: run separate `machin test` invocations for separate suites.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// TestRunResult is `machin test`'s whole verdict — one composed program, one
// tally, matching the "N passed, M failed" a Go `go test` run reports for one
// package.
type TestRunResult struct {
	OK     bool     `json:"ok"`
	Passed int      `json:"passed"`
	Failed int      `json:"failed"`
	Files  []string `json:"files"`
}

// runMFLTests is the pure core of `machin test` — no exit, no direct I/O — so
// it's unit-testable the same way analyzeSource (machin check's core) is.
// Composes framework/test.src ahead of files, builds+runs the result as one
// program, and parses its TEST_SUMMARY tally. programOutput is the test
// program's own stdout+stderr (FAIL lines etc.) — the caller decides where it
// goes; keeping it out of TestRunResult keeps --json output pure JSON.
func runMFLTests(files []string) (res TestRunResult, programOutput string, err error) {
	if len(files) == 0 {
		return res, "", fmt.Errorf("test: need at least one .src/.mfl test file")
	}
	res.Files = files

	prog, _, err := composeSources(append([]string{"framework/test.src"}, files...))
	if err != nil {
		return res, "", err
	}
	bin, err := os.CreateTemp("", "mfl-test-*")
	if err != nil {
		return res, "", err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		return res, "", err
	}
	out, _ := exec.Command(bin.Name()).CombinedOutput() // exit code is redundant with the parsed tally
	programOutput = string(out)
	passed, failed, ok := parseTestSummary(programOutput)
	if !ok {
		return res, programOutput, fmt.Errorf("test: no TEST_SUMMARY line in output — did the test call test_summary()?")
	}
	res.Passed, res.Failed, res.OK = passed, failed, failed == 0
	return res, programOutput, nil
}

func cmdTest(args []string) error {
	jsonOut := false
	var files []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else {
			files = append(files, a)
		}
	}

	res, programOutput, err := runMFLTests(files)
	// The test program's own output (FAIL lines, the TEST_SUMMARY line) is
	// diagnostic detail, not the answer — stderr always, so --json's stdout
	// stays pure JSON (machin check's same agent-first convention).
	if programOutput != "" {
		fmt.Fprint(os.Stderr, programOutput)
	}
	if err != nil {
		return err
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout) // no HTML escaping — messages are full of < > &
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(os.Stderr, "\n%d passed, %d failed\n", res.Passed, res.Failed)
	}
	if !res.OK {
		os.Exit(1)
	}
	return nil
}

// parseTestSummary finds the LAST "TEST_SUMMARY passed=N failed=M" line in a
// test program's output (framework/test.src's test_summary() prints it as
// the final line on success, or before exit(1) on failure — take the last
// occurrence in case earlier program output happens to contain the prefix).
func parseTestSummary(out string) (passed, failed int, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "TEST_SUMMARY ") {
			continue
		}
		var p, f int
		var pOK, fOK bool
		for _, field := range strings.Fields(line)[1:] {
			if v, found := strings.CutPrefix(field, "passed="); found {
				if n, err := strconv.Atoi(v); err == nil {
					p, pOK = n, true
				}
			} else if v, found := strings.CutPrefix(field, "failed="); found {
				if n, err := strconv.Atoi(v); err == nil {
					f, fOK = n, true
				}
			}
		}
		if pOK && fOK {
			passed, failed, ok = p, f, true
		}
	}
	return
}
