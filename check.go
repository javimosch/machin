package main

// `machin check [--json] <file.src|.mfl> ... | --stdin` — agent-native diagnostics.
// Runs lex -> parse -> typecheck ONLY (no codegen, no cc), and reports the checker's
// verdict as structured, machine-parseable data. This is the machine-first analog of an
// LSP: no editor, no UI — just fast, structured truth an agent can act on in a
// write -> check -> fix loop. See docs/check-json.md.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Diagnostic is one problem the checker found. `Code` is the stable contract an agent
// branches on; `Message` is human detail (don't pattern-match it). `Decl` is the
// declaration it's in — the natural fix unit for a one-declaration-per-line language.
type Diagnostic struct {
	Severity string `json:"severity"`          // "error" (only value in v1)
	Phase    string `json:"phase"`             // "lex" | "parse" | "typecheck"
	Code     string `json:"code"`              // stable machine code (branch on this)
	Message  string `json:"message"`           // human-readable detail
	Decl     string `json:"decl,omitempty"`    // the function / type / global it's in
	Line     int    `json:"line,omitempty"`    // 1-based source line where the decl starts
	Snippet  string `json:"snippet,omitempty"` // the offending source fragment
}

// CheckResult is the whole verdict: one JSON object, never streamed.
type CheckResult struct {
	OK          bool         `json:"ok"`
	Files       []string     `json:"files"`
	ErrorCount  int          `json:"errorCount"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func cmdCheck(args []string) error {
	jsonOut, stdin := false, false
	var files []string
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--stdin":
			stdin = true
		case "--symbols":
			// reserved for a future outline mode; v1 ignores it
		default:
			files = append(files, a)
		}
	}

	// gather source (multiple files are concatenated like encode/build)
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
			return fmt.Errorf("check: need a source file or --stdin")
		}
		for _, p := range files {
			data, err := readModule(p) // local file, else an embedded framework module
			if err != nil {
				return emitCheck(CheckResult{OK: false, Files: files, ErrorCount: 1, Diagnostics: []Diagnostic{{
					Severity: "error", Phase: "lex", Code: "file-not-found", Message: err.Error(), Decl: p,
				}}}, jsonOut)
			}
			combined.Write(data)
			combined.WriteByte('\n')
		}
		srcNames = files
	}

	return emitCheck(analyzeSource(combined.String(), srcNames), jsonOut)
}

// analyzeSource is the pure core: source text -> verdict. No I/O, no exit (so tests can
// call it directly). lex -> parse (per declaration, collecting all errors) -> typecheck.
func analyzeSource(combined string, srcNames []string) CheckResult {
	diags := []Diagnostic{}

	// split into declaration blocks, tracking each one's start line for reporting
	blocks, blockLines, err := splitFunctionsLoc(combined)
	if err != nil {
		diags = append(diags, Diagnostic{Severity: "error", Phase: "parse", Code: "parse-unbalanced-braces", Message: err.Error()})
		return CheckResult{OK: false, Files: srcNames, ErrorCount: len(diags), Diagnostics: diags}
	}

	// normalize + seed all struct/cstruct names so per-decl parsing sees `T{...}` literals
	decls := make([]string, len(blocks))
	structs := map[string]bool{}
	for i, b := range blocks {
		decls[i] = normalize(b)
	}
	for _, d := range decls {
		seedStructNames(d, structs)
	}

	// parse phase: parse each declaration independently -> collect ALL parse errors
	parseOK := true
	for i, d := range decls {
		if perr := parseOneDecl(d, structs); perr != nil {
			parseOK = false
			diags = append(diags, Diagnostic{
				Severity: "error", Phase: "parse",
				Code:    classifyParse(perr.Error()),
				Message: perr.Error(),
				Decl:    declName(d),
				Line:    blockLines[i],
				Snippet: firstMeaningfulLine(blocks[i]),
			})
		}
	}

	// typecheck phase (only if parsing is clean): the checker bails on the first error,
	// so v1 reports a single typecheck diagnostic (v2 = accumulate — see the spec).
	if parseOK {
		prog, perr := ParseProgram(decls)
		if perr != nil {
			diags = append(diags, Diagnostic{Severity: "error", Phase: "parse", Code: classifyParse(perr.Error()), Message: perr.Error()})
		} else if _, cerr := Check(prog); cerr != nil {
			msg := strings.TrimPrefix(cerr.Error(), "typecheck: ")
			decl, line := locateCheckError(msg, decls, blockLines)
			diags = append(diags, Diagnostic{
				Severity: "error", Phase: "typecheck",
				Code:    classifyCheck(msg),
				Message: msg,
				Decl:    decl,
				Line:    line,
			})
		}
	}

	return CheckResult{OK: len(diags) == 0, Files: srcNames, ErrorCount: len(diags), Diagnostics: diags}
}

// emitCheck writes the verdict (JSON to stdout, or human text to stderr) and exits
// non-zero if there were any errors.
func emitCheck(res CheckResult, jsonOut bool) error {
	if jsonOut {
		// no HTML escaping: machin code and messages are full of < > & (operators),
		// and an agent parsing the JSON shouldn't get < noise.
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return err
		}
	} else if res.OK {
		fmt.Fprintln(os.Stderr, "ok — no errors")
	} else {
		for _, d := range res.Diagnostics {
			loc := ""
			if d.Decl != "" {
				loc = " in " + d.Decl
			}
			if d.Line > 0 {
				loc += fmt.Sprintf(" (line %d)", d.Line)
			}
			fmt.Fprintf(os.Stderr, "%s: [%s] %s%s\n", d.Severity, d.Code, d.Message, loc)
		}
	}
	if !res.OK {
		os.Exit(1)
	}
	return nil
}

// splitFunctionsLoc is splitFunctions + the 1-based source line where each block starts.
func splitFunctionsLoc(src string) ([]string, []int, error) {
	var funcs []string
	var startLines []int
	var cur strings.Builder
	depth := 0
	started := false
	startLine := 0
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !started {
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			started = true
			startLine = i + 1
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		inStr := false
		for j := 0; j < len(line); j++ {
			c := line[j]
			if inStr {
				if c == '\\' {
					j++
				} else if c == '"' {
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '/':
				if j+1 < len(line) && line[j+1] == '/' {
					j = len(line)
				}
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		body := strings.TrimSpace(cur.String())
		if started && depth == 0 && (strings.Contains(body, "{") || strings.HasPrefix(body, "var ")) {
			funcs = append(funcs, body)
			startLines = append(startLines, startLine)
			cur.Reset()
			started = false
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		return nil, nil, fmt.Errorf("unbalanced braces near: %s", strings.TrimSpace(cur.String()))
	}
	return funcs, startLines, nil
}

// seedStructNames records every `type X` / `cstruct X` name so struct-literal parsing works.
func seedStructNames(decl string, structs map[string]bool) {
	toks, err := Lex(decl)
	if err != nil {
		return
	}
	for i := 0; i+1 < len(toks); i++ {
		if toks[i].Kind == TKeyword && (toks[i].Val == "type" || toks[i].Val == "cstruct") && toks[i+1].Kind == TIdent {
			structs[toks[i+1].Val] = true
		}
	}
}

// parseOneDecl parses a single normalized declaration, routing by its leading keyword.
func parseOneDecl(decl string, structs map[string]bool) error {
	toks, err := Lex(decl)
	if err != nil {
		return err
	}
	if len(toks) > 0 && toks[0].Kind == TKeyword {
		switch toks[0].Val {
		case "type":
			_, e := ParseType(decl)
			return e
		case "extern":
			_, e := ParseExtern(decl)
			return e
		case "var":
			_, e := ParseGlobalWith(decl, structs)
			return e
		}
	}
	_, e := ParseFuncWith(decl, structs) // func / export func
	return e
}

// declName extracts the declared name (skips `export`, the keyword, takes the ident).
func declName(decl string) string {
	toks, err := Lex(decl)
	if err != nil {
		return ""
	}
	i := 0
	if i < len(toks) && toks[i].Kind == TKeyword && toks[i].Val == "export" {
		i++
	}
	if i < len(toks) && toks[i].Kind == TKeyword {
		i++
	}
	if i < len(toks) && toks[i].Kind == TIdent {
		return toks[i].Val
	}
	return ""
}

// firstMeaningfulLine is the first non-blank, non-comment source line of a block (a snippet).
func firstMeaningfulLine(block string) string {
	for _, l := range strings.Split(block, "\n") {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "//") {
			if len(t) > 120 {
				t = t[:117] + "..."
			}
			return t
		}
	}
	return ""
}

// locateCheckError best-effort maps a typecheck message to the declaration it names
// (checker messages quote names, e.g. `function "foo" ...`), returning its start line.
func locateCheckError(msg string, decls []string, lines []int) (string, int) {
	for i, d := range decls {
		name := declName(d)
		if name != "" && strings.Contains(msg, `"`+name+`"`) {
			return name, lines[i]
		}
	}
	return "", 0
}

func classifyParse(msg string) string {
	switch {
	case strings.Contains(msg, "unexpected token"):
		return "parse-unexpected-token"
	case strings.Contains(msg, "expected"):
		return "parse-expected"
	case strings.Contains(msg, "unterminated"):
		return "parse-unterminated-string"
	case strings.Contains(msg, "unbalanced"):
		return "parse-unbalanced-braces"
	default:
		return "parse-error"
	}
}

func classifyCheck(msg string) string {
	switch {
	case strings.Contains(msg, "type mismatch"):
		return "type-mismatch"
	case strings.Contains(msg, "shadows the builtin"):
		return "shadows-builtin"
	case strings.Contains(msg, "no main function"):
		return "no-main"
	case strings.Contains(msg, "duplicate type"):
		return "duplicate-type"
	case strings.Contains(msg, "field"):
		return "undefined-field"
	case strings.Contains(msg, "undefined") || strings.Contains(msg, "unknown") || strings.Contains(msg, "not defined"):
		return "undefined-name"
	case strings.Contains(msg, "argument") || strings.Contains(msg, "expects") || strings.Contains(msg, "arity"):
		return "arity-mismatch"
	case strings.Contains(msg, "unsupported"):
		return "unsupported-construct"
	default:
		return "type-error"
	}
}
