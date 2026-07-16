package main

// falsifycmd.go — the `machin falsify` driver (Slice 1.2).
//
//	machin falsify <file>...            human-readable counterexamples + coverage
//	machin falsify --json <file>...     the verdict envelope as JSON
//	machin falsify --repro <dir> <f>    write one runnable .mfl repro per finding
//
// Advisory by design: exit code is 0 even when counterexamples are found (a
// bounded finder must not gate CI by default). A `--strict` mode that exits
// non-zero on any counterexample lands in Slice 1.4.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type falsifyCoverage struct {
	Checked    int `json:"checked"`    // functions fully enumerated (found or clean)
	Skipped    int `json:"skipped"`    // out-of-scope param type
	AllUnknown int `json:"allUnknown"` // every input inconclusive (e.g. calls, FFI)
}

// falsifyBounds records the (fixed) search envelope so a `clean`/`unknown` verdict
// is honestly qualified: "no bug within THESE bounds", never "proved correct".
type falsifyBounds struct {
	SliceLenMax int     `json:"sliceLenMax"`
	IntDomain   []int64 `json:"intDomain"`
	CallDepth   int     `json:"callDepth"`
}

// falsifyReport is the JSON verdict envelope. It never claims `proved` — coverage,
// the per-function verdicts, and the bounds tell the agent exactly what was checked.
type falsifyReport struct {
	OK              bool            `json:"ok"` // no counterexamples found
	Files           []string        `json:"files"`
	Counterexamples int             `json:"counterexamples"`
	Findings        []Diagnostic    `json:"findings"`
	Coverage        falsifyCoverage `json:"coverage"`
	Functions       []funcVerdict   `json:"functions"`
	Bounds          falsifyBounds   `json:"bounds"`
}

func cmdFalsify(args []string) error {
	jsonOut, stdin, strict := false, false, false
	reproDir := ""
	var files []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOut = true
		case "--stdin":
			stdin = true
		case "--strict":
			strict = true
		case "--prove":
			falsProve = true
		case "--repro":
			if i+1 >= len(args) {
				return fmt.Errorf("falsify: --repro needs a directory")
			}
			i++
			reproDir = args[i]
		default:
			files = append(files, args[i])
		}
	}

	// gather source (concatenate like check/build)
	var combined strings.Builder
	var srcNames []string
	if stdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		combined.Write(data)
		combined.WriteByte('\n')
		srcNames = []string{"<stdin>"}
	} else {
		if len(files) == 0 {
			return fmt.Errorf("falsify: need a source file or --stdin")
		}
		for _, p := range files {
			data, err := readModule(p)
			if err != nil {
				return fmt.Errorf("falsify: %w", err)
			}
			combined.Write(data)
			combined.WriteByte('\n')
		}
		srcNames = files
	}

	// lex/parse/typecheck: falsify only runs on programs that already compile.
	blocks, blockLines, err := splitFunctionsLoc(combined.String())
	if err != nil {
		return fmt.Errorf("falsify: %w", err)
	}
	decls := make([]string, len(blocks))
	for i, b := range blocks {
		decls[i] = normalize(b)
	}
	prog, perr := ParseProgram(decls)
	if perr != nil {
		return fmt.Errorf("falsify: parse: %w", perr)
	}
	c, cerr := Check(prog)
	if cerr != nil {
		return fmt.Errorf("falsify: %w (falsify needs a program that typechecks)", cerr)
	}

	declLine := map[string]int{}
	for i, d := range decls {
		declLine[declName(d)] = blockLines[i]
	}

	findings, stats, verdicts := detectFalsifiableStats(prog, c)

	// write repros if requested
	if reproDir != "" {
		if err := os.MkdirAll(reproDir, 0o755); err != nil {
			return fmt.Errorf("falsify: --repro: %w", err)
		}
		for i, ff := range findings {
			path := filepath.Join(reproDir, fmt.Sprintf("repro_%s_%d.mfl", ff.Decl, i+1))
			src := reproProgram(decls, ff.Decl, ff.args)
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				return fmt.Errorf("falsify: --repro: %w", err)
			}
		}
	}

	bounds := falsifyBounds{SliceLenMax: falsSliceLenMax, IntDomain: falsIntDomain, CallDepth: falsCallDepth}
	if falsProve {
		bounds = falsifyBounds{SliceLenMax: falsProveSliceLenMax, IntDomain: falsProveIntDomain(), CallDepth: falsCallDepth}
	}
	rep := falsifyReport{
		OK:              len(findings) == 0,
		Files:           srcNames,
		Counterexamples: len(findings),
		Findings:        make([]Diagnostic, 0, len(findings)),
		Coverage:        falsifyCoverage{Checked: stats.Checked, Skipped: stats.Skipped, AllUnknown: stats.AllUnknown},
		Functions:       verdicts,
		Bounds:          bounds,
	}
	for _, ff := range findings {
		d := ff.toDiagnostic()
		d.Line = declLine[ff.Decl]
		rep.Findings = append(rep.Findings, d)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else if rep.OK {
		fmt.Fprintf(os.Stderr, "no counterexamples (checked %d, skipped %d, inconclusive %d)\n",
			stats.Checked, stats.Skipped, stats.AllUnknown)
	} else {
		for _, ff := range findings {
			loc := ff.Decl
			if l := declLine[ff.Decl]; l > 0 {
				loc += fmt.Sprintf(" (line %d)", l)
			}
			fmt.Fprintf(os.Stderr, "[%s] %s at `%s` when %s — in %s\n",
				ff.Code, ff.Prop, ff.Expr, ff.Bind, loc)
		}
		fmt.Fprintf(os.Stderr, "%d counterexample(s); checked %d, skipped %d, inconclusive %d\n",
			len(findings), stats.Checked, stats.Skipped, stats.AllUnknown)
		if reproDir != "" {
			fmt.Fprintf(os.Stderr, "repros written to %s/\n", reproDir)
		}
	}

	// --strict: advisory by default, but CI can opt in to a non-zero exit on any
	// counterexample. Only counterexamples gate — `unknown`/`skipped` never do
	// (the finder is unsound-complete; absence of a bug is not a proof of safety).
	if strict && len(findings) > 0 {
		os.Exit(1)
	}
	return nil
}
