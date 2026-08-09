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
	OK       bool      `json:"ok"`
	Passed   int       `json:"passed"`
	Failed   int       `json:"failed"`
	Files    []string  `json:"files"`
	Coverage *Coverage `json:"coverage,omitempty"`
}

// Coverage is `machin test --cover`'s report (#589, #236 stage B1).
//
// Kind is stated explicitly and is not optional. A coverage number that cannot
// say what it measured is worse than no number: "function" here means a function
// was ENTERED at least once, which is much weaker than statement or branch
// coverage and must never be quoted as if it were either.
type Coverage struct {
	Kind    string          `json:"kind"` // always "function" in B1
	Covered int             `json:"covered"`
	Total   int             `json:"total"`
	Pct     float64         `json:"pct"`
	Files   []FileCoverage  `json:"files"`
}

// FileCoverage is one source file's share of the report. Uncovered names are
// listed rather than merely counted — the point of the number is to say WHICH
// functions no test reaches.
type FileCoverage struct {
	File      string   `json:"file"`
	Covered   int      `json:"covered"`
	Total     int      `json:"total"`
	Uncovered []string `json:"uncovered"`
}

// runMFLTests is the pure core of `machin test` — no exit, no direct I/O — so
// it's unit-testable the same way analyzeSource (machin check's core) is.
// Composes framework/test.src ahead of files, builds+runs the result as one
// program, and parses its TEST_SUMMARY tally. programOutput is the test
// program's own stdout+stderr (FAIL lines etc.) — the caller decides where it
// goes; keeping it out of TestRunResult keeps --json output pure JSON.
func runMFLTests(files []string, cover bool) (res TestRunResult, programOutput string, err error) {
	if len(files) == 0 {
		return res, "", fmt.Errorf("test: need at least one .src/.mfl test file")
	}
	res.Files = files

	all := append([]string{"framework/test.src"}, files...)
	prog, _, err := composeSources(all)
	if err != nil {
		return res, "", err
	}
	if cover {
		coverFuncs = true
		defer func() { coverFuncs = false }()
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
	cmd := exec.Command(bin.Name())
	var covPath string
	if cover {
		cf, ferr := os.CreateTemp("", "mfl-cov-*")
		if ferr != nil {
			return res, "", ferr
		}
		cf.Close()
		covPath = cf.Name()
		defer os.Remove(covPath)
		cmd.Env = append(os.Environ(), "MFL_COVER_OUT="+covPath)
	}
	out, _ := cmd.CombinedOutput() // exit code is redundant with the parsed tally
	programOutput = string(out)
	passed, failed, ok := parseTestSummary(programOutput)
	if !ok {
		return res, programOutput, fmt.Errorf("test: no TEST_SUMMARY line in output — did the test call test_summary()?")
	}
	res.Passed, res.Failed, res.OK = passed, failed, failed == 0
	if cover {
		cov, cerr := readCoverage(covPath, prog, all)
		if cerr != nil {
			return res, programOutput, cerr
		}
		res.Coverage = cov
	}
	return res, programOutput, nil
}

// readCoverage turns the counter dump into a per-file report.
//
// The dump only carries names, so file attribution comes from a separate scan of
// the composed sources (declOwners). A function present in the AST but missing
// from the dump is a MISS, not an omission: machin instantiates only functions
// something calls, so an unreached function never gets a counter emitted at all.
// That is the case the report most needs to show, so absence is treated as
// uncovered rather than skipped.
func readCoverage(path string, prog *Program, files []string) (*Coverage, error) {
	hits := map[string]bool{}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			name, ok := strings.CutPrefix(line, "hit ")
			if ok {
				hits[strings.TrimSpace(name)] = true
			}
		}
	}
	owners := declOwners(files)
	cov := &Coverage{Kind: "function"}
	byFile := map[string]*FileCoverage{}
	var order []string
	for _, fn := range prog.Funcs {
		if fn.IsLambda {
			continue
		}
		file := owners[fn.Name]
		if file == "" {
			file = "(unknown)"
		}
		fc, seen := byFile[file]
		if !seen {
			fc = &FileCoverage{File: file}
			byFile[file] = fc
			order = append(order, file)
		}
		fc.Total++
		cov.Total++
		if hits[fn.Name] {
			fc.Covered++
			cov.Covered++
		} else {
			fc.Uncovered = append(fc.Uncovered, fn.Name)
		}
	}
	for _, f := range order {
		cov.Files = append(cov.Files, *byFile[f])
	}
	if cov.Total > 0 {
		cov.Pct = float64(cov.Covered) / float64(cov.Total) * 100
	}
	return cov, nil
}

// declOwners maps a declared function name to the file it came from.
// composeSources concatenates every input into one string before parsing, so the
// AST cannot say which file a function came from; this re-reads each input and
// records the `func <name>` declarations it opens. Text-level on purpose — it
// needs only the names, and reproducing the parser here would be worse.
func declOwners(files []string) map[string]string {
	owners := map[string]string{}
	for _, path := range files {
		data, err := readModule(path)
		if err != nil {
			continue
		}
		blocks, err := splitFunctions(string(data))
		if err != nil {
			continue
		}
		for _, b := range blocks {
			rest, ok := strings.CutPrefix(strings.TrimSpace(normalize(b)), "func ")
			if !ok {
				continue
			}
			if i := strings.IndexAny(rest, "("); i > 0 {
				name := strings.TrimSpace(rest[:i])
				if name != "" {
					if _, dup := owners[name]; !dup {
						owners[name] = path
					}
				}
			}
		}
	}
	return owners
}

func cmdTest(args []string) error {
	jsonOut, cover := false, false
	var files []string
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--cover":
			cover = true
		default:
			files = append(files, a)
		}
	}

	res, programOutput, err := runMFLTests(files, cover)
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
		if c := res.Coverage; c != nil {
			// "function coverage" spelled out every time: this counts functions
			// ENTERED, not statements or branches, and must not be quoted as either.
			fmt.Fprintf(os.Stderr, "\nfunction coverage: %d/%d (%.1f%%)\n", c.Covered, c.Total, c.Pct)
			for _, f := range c.Files {
				fmt.Fprintf(os.Stderr, "  %-40s %d/%d\n", f.File, f.Covered, f.Total)
				if len(f.Uncovered) > 0 {
					fmt.Fprintf(os.Stderr, "      uncovered: %s\n", strings.Join(f.Uncovered, ", "))
				}
			}
		}
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
