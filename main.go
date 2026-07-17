// machin — the MFL (Machine-First Language) compiler.
//
// MFL is a backend language based on Go but machine-first: minimal syntax, no
// type annotations (types are inferred), one canonical function per line. The
// human states intent; agents read and write the code. The .mfl source of truth
// is plain canonical text — one normalized function per line, a blank line
// between functions — so it stays greppable, diffable, and cheap for an agent
// to edit. A dense base64 "packed" form is available via `machin pack` for
// distribution, and `machin run` reads either form. MFL is statically typed (by
// inference) and compiles to native code through C, so it runs at C/Rust/Zig
// speed for scalar work.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "build":
		err = cmdBuild(os.Args[2:])
	case "encode":
		err = cmdEncode(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:]) // agent-native diagnostics (lex/parse/typecheck + advisory falsify, JSON)
	case "falsify":
		err = cmdFalsify(os.Args[2:]) // bounded counterexamples: the exact input that breaks a function
	case "certify":
		err = cmdCertify(os.Args[2:]) // translation validation: prove the compiler matched the source (within bounds)
	case "test":
		err = cmdTest(os.Args[2:]) // native MFL test runner (framework/test.src assert helpers)
	case "pack":
		err = cmdPack(os.Args[2:])
	case "guide":
		err = cmdGuide(os.Args[2:])
	case "skill":
		err = cmdSkill(os.Args[2:])
	case "cgentest":
		if err := cmdCGenTest(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "racetest":
		if err := cmdRaceTest(os.Args[2:]); err != nil { // self-hosting oracle: dump race findings canonically
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "falsifytest":
		if err := cmdFalsifyTest(os.Args[2:]); err != nil { // self-hosting oracle: dump falsify findings canonically
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "deadlocktest":
		if err := cmdDeadlockTest(os.Args[2:]); err != nil { // self-hosting oracle: dump DL001 findings canonically
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "uftest":
		if err := cmdUFTest(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "lexbench":
		if err := cmdLexBench(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	case "lextest":
		err = cmdLexTest(os.Args[2:]) // self-hosting oracle (selfhost/): dump the Go token stream
	case "parsetest":
		err = cmdParseTest(os.Args[2:]) // self-hosting oracle (selfhost/): dump the Go AST as S-exprs
	case "checktest":
		err = cmdCheckTest(os.Args[2:]) // self-hosting oracle (selfhost/): dump inferred types
	case "framework":
		err = cmdFramework(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `machin — Machine-First Language compiler

usage:
  machin run   <file.mfl>            compile to native + execute
  machin build <file.mfl> [-o out]   compile to a native binary
  machin build <file.mfl> --target wasm  compile to a WebAssembly module (needs zig; mark exports with export func)
  machin build <file.mfl> --static   fully static binary (bundles SQLite; pair with CC=musl-gcc for FROM scratch)
  machin build <file.mfl> --emit-c   print the generated C and stop
  machin build|run <file.mfl> --safe  insert bounds / div-zero / overflow checks
  machin build|run <file.mfl> --race-safe  refuse to build if a data race is inferred
  machin check <src...>|--stdin      lex+parse+typecheck only (no cc); add --json for machine-readable diagnostics
  machin test [--json] <src...>      run MFL test files (framework/test.src: assert/assert_eq_int/assert_eq_str)
  machin encode <src>                mint canonical MFL from loose Go-like text (framework/*.src resolve from the binary)
  machin framework list|<name>|--vendor   the embedded framework modules (machweb, db drivers, …)
  machin pack  <file.mfl>            emit the dense base64 form (distribution)
  machin guide                       full feature catalog as JSON (--text for prose)
  machin skill install               register the agent skills where coding agents look

Agents: run "machin guide" for the complete, version-exact feature surface —
keywords, every builtin with signature, idioms, and gotchas — in one call.

A .mfl program is canonical plain text: one normalized function per line, a
blank line between functions. `+"`machin run`"+` also reads the packed base64 form.
`)
}

// loadMFL reads a .mfl file and parses it into a Program (struct types and
// functions). Accepts canonical plain text, the packed base64 form, and
// loose/spaced multi-line source — see loadDecls.
func loadMFL(path string) (*Program, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decls, err := loadDecls(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(prog.Funcs) == 0 {
		return nil, fmt.Errorf("%s: no functions", path)
	}
	return prog, nil
}

// loadDecls turns .mfl file content into one normalized declaration per
// element, tolerating three source shapes:
//   - the packed base64 form (`machin pack`'s distribution form: one
//     base64-encoded declaration per line — recognizable because base64 has
//     no whitespace, so a file where EVERY non-blank line lacks whitespace is
//     treated as fully packed);
//   - canonical plain text (already one normalized declaration per line);
//   - loose/spaced multi-line source (ordinary Go-like formatting, a
//     declaration's body spanning several physical lines).
//
// The last two are handled identically, via splitFunctionsLoc + normalize —
// the exact machinery `machin encode`/`check` already use, so `machin
// build`/`run` tolerate hand-written .mfl the same way `check` already does,
// instead of misreading a line like `println(x)` (no whitespace, but plainly
// not base64) as a malformed packed declaration. (Found via dogfooding.)
func loadDecls(content string) ([]string, error) {
	allPacked, sawLine := true, false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sawLine = true
		if strings.ContainsAny(line, " \t") {
			allPacked = false
			break
		}
	}
	if sawLine && allPacked {
		var decls []string
		for n, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: not valid packed (base64) MFL: %w", n+1, err)
			}
			decls = append(decls, string(raw))
		}
		return decls, nil
	}
	blocks, _, err := splitFunctionsLoc(content)
	if err != nil {
		return nil, err
	}
	decls := make([]string, len(blocks))
	for i, b := range blocks {
		decls[i] = normalize(b)
	}
	return decls, nil
}

// declFromLine yields one declaration from a .mfl line. Canonical MFL is plain
// text — one normalized function per line — which always contains whitespace.
// A line with no whitespace is a packed (base64) declaration, accepted for the
// distribution form produced by `machin pack`.
func declFromLine(line string) (string, error) {
	if strings.ContainsAny(line, " \t") {
		return line, nil // plain canonical text
	}
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		return "", fmt.Errorf("line is neither plain MFL nor base64: %w", err)
	}
	return string(raw), nil
}

// raceGate runs the inferred data-race analysis and returns a non-nil error listing
// every race, for use under `--race-safe` (option a: build/run refuses on a race).
func raceGate(prog *Program) error {
	c, err := Check(prog)
	if err != nil {
		return nil // let the normal compile path report the type error
	}
	fs := detectRaces(prog, c)
	if len(fs) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "data race(s) detected (--race-safe): %d\n", len(fs))
	for _, rf := range fs {
		fmt.Fprintf(&b, "  %s in %s(): data race on `%s` — %s\n",
			rf.Kind, rf.Decl, rf.Root, strings.Join(rf.Writers, "; "))
	}
	return fmt.Errorf("%s", b.String())
}

func cmdRun(args []string) error {
	safe, raceSafe, verify := false, false, false
	var src, recordTrace, replayTrace string
	jsonReport := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--safe":
			safe = true
		case "--race-safe":
			raceSafe = true
		case "--verify":
			verify = true
		case "--json":
			jsonReport = true
		case "--record":
			if i+1 >= len(args) {
				return fmt.Errorf("run: --record needs a trace file")
			}
			i++
			recordTrace = args[i]
		case "--replay":
			if i+1 >= len(args) {
				return fmt.Errorf("run: --replay needs a trace file")
			}
			i++
			replayTrace = args[i]
		default:
			src = args[i]
		}
	}
	if src == "" {
		return fmt.Errorf("run: need exactly one .mfl file")
	}
	prog, err := loadMFL(src)
	if err != nil {
		return err
	}
	if raceSafe {
		if err := raceGate(prog); err != nil {
			return err
		}
	}
	bin, err := os.CreateTemp("", "mfl-run-*")
	if err != nil {
		return err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), safe); err != nil {
		return err
	}
	cmd := exec.Command(bin.Name())
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	cmd.Env = os.Environ()
	if recordTrace != "" {
		abs, _ := filepath.Abs(recordTrace)
		srcAbs, _ := filepath.Abs(src)
		safeFlag := "0"
		if safe {
			safeFlag = "1"
		}
		cmd.Env = append(cmd.Env, "MFL_RR_RECORD="+abs, "MFL_RR_SRC="+srcAbs, "MFL_RR_SAFE="+safeFlag)
	}
	if replayTrace != "" {
		abs, _ := filepath.Abs(replayTrace)
		cmd.Env = append(cmd.Env, "MFL_RR_REPLAY="+abs)
	}
	if verify {
		cmd.Env = append(cmd.Env, "MFL_RR_VERIFY=1")
	}
	if jsonReport {
		cmd.Env = append(cmd.Env, "MFL_RR_JSON=1") // deadlock / crash reported as a JSON causal artifact
	}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}

// cmdReplay re-executes a recorded trace. The trace carries its program path, so
// you replay a run without re-naming the source. A recorded crash reproduces
// exactly; --json turns it into a structured causal report an agent can read.
func cmdReplay(args []string) error {
	jsonOut, verify := false, false
	var trace string
	var printVars []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json":
			jsonOut = true
		case "--verify":
			verify = true
		case "--print":
			// value-query debugger: --print <var>[,<var>...] emits a probe after each
			// assignment to those variables, so replay shows their deterministic history.
			if i+1 >= len(args) {
				return fmt.Errorf("replay: --print needs a variable name (e.g. --print balance)")
			}
			i++
			for _, v := range strings.Split(args[i], ",") {
				if v = strings.TrimSpace(v); v != "" {
					printVars = append(printVars, v)
				}
			}
		default:
			trace = a
		}
	}
	if trace == "" {
		return fmt.Errorf("replay: need a trace file (from `machin run --record <trace> <file>`)")
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		return err
	}
	var src string
	safe := false
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(ln, "program ") {
			src = strings.TrimSpace(ln[len("program "):])
		} else if strings.TrimSpace(ln) == "safe 1" {
			safe = true // reproduce a crash the recorded --safe run caught
		}
	}
	if src == "" {
		return fmt.Errorf("replay: trace has no `program` line — record it with `machin run --record`")
	}
	prog, err := loadMFL(src)
	if err != nil {
		return err
	}
	bin, err := os.CreateTemp("", "mfl-replay-*")
	if err != nil {
		return err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	// --print watches variables: instrument the rebuild for probes, then reset the global
	// so it never leaks into any other build in this process.
	debugProbeVars = printVars
	err = BuildBinary(prog, bin.Name(), safe)
	debugProbeVars = nil
	if err != nil {
		return err
	}
	traceAbs, _ := filepath.Abs(trace)
	cmd := exec.Command(bin.Name())
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	cmd.Env = append(os.Environ(), "MFL_RR_REPLAY="+traceAbs)
	if jsonOut {
		cmd.Env = append(cmd.Env, "MFL_RR_JSON=1")
	}
	if verify {
		cmd.Env = append(cmd.Env, "MFL_RR_VERIFY=1")
	}
	if len(printVars) > 0 {
		cmd.Env = append(cmd.Env, "MFL_RR_PROBE=1")
	}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}

func cmdBuild(args []string) error {
	var src, out, target string
	emitC, safe, static, raceSafe := false, false, false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("build: -o needs a path")
			}
			i++
			out = args[i]
		case "--target":
			if i+1 >= len(args) {
				return fmt.Errorf("build: --target needs a value (native|wasm)")
			}
			i++
			target = args[i]
		case "--emit-c":
			emitC = true
		case "--safe":
			safe = true
		case "--static":
			static = true
		case "--race-safe":
			raceSafe = true
		default:
			src = args[i]
		}
	}
	if src == "" {
		return fmt.Errorf("build: need a .mfl file")
	}
	switch target {
	case "", "native":
		target = "native"
	case "wasm":
		if static {
			return fmt.Errorf("build: --static applies to the native target, not wasm")
		}
	default:
		return fmt.Errorf("build: unknown --target %q (want native or wasm)", target)
	}
	prog, err := loadMFL(src)
	if err != nil {
		return err
	}
	if raceSafe {
		if err := raceGate(prog); err != nil {
			return err
		}
	}
	if emitC {
		c, _, err := CompileToCTarget(prog, safe, target)
		if err != nil {
			return err
		}
		fmt.Print(c)
		return nil
	}
	if target == "wasm" {
		if out == "" {
			out = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)) + ".wasm"
		}
		if err := BuildWasm(prog, out, safe); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "built %s (wasm)\n", out)
		return nil
	}
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}
	if err := BuildBinaryStatic(prog, out, safe, static); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "built %s\n", out)
	return nil
}

// cmdEncode lifts loose Go-like text into canonical MFL: one normalized function
// per line, a blank line between functions. Multiple source files are
// concatenated in order, so a framework can be composed with an app:
//
//	machin encode framework/machweb.src myapp.src > app.mfl
func cmdEncode(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("encode: need at least one source file")
	}
	_, out, err := composeSources(args)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// composeSources reads and concatenates one or more sources (local files or
// embedded framework modules, resolved via readModule), splits them into
// per-function blocks, normalizes each into canonical form, and parses +
// typechecks the result. Returns the typechecked Program and the canonical
// text (what `machin encode` prints) — shared by cmdEncode and cmdTest so
// composing a framework module ahead of a file behaves identically in both.
func composeSources(paths []string) (*Program, string, error) {
	var combined strings.Builder
	for _, path := range paths {
		data, err := readModule(path) // local file, else an embedded framework module
		if err != nil {
			return nil, "", err
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}
	blocks, err := splitFunctions(combined.String())
	if err != nil {
		return nil, "", err
	}
	var decls []string
	var out strings.Builder
	for _, b := range blocks {
		n := normalize(b)
		decls = append(decls, n)
		out.WriteString(n)
		out.WriteString("\n\n")
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		return nil, "", fmt.Errorf("parse: %w", err)
	}
	if _, err := Check(prog); err != nil {
		return nil, "", fmt.Errorf("typecheck: %w", err)
	}
	return prog, out.String(), nil
}

// cmdPack emits the dense base64 "packed" form of a .mfl: one base64 line per
// declaration. The plain-text .mfl is the source of truth; pack is only for a
// compact wire/distribution artifact. `machin run` reads either form.
func cmdPack(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("pack: need one .mfl file")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var out strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		decl, err := declFromLine(line) // tolerate already-packed input
		if err != nil {
			return err
		}
		out.WriteString(base64.StdEncoding.EncodeToString([]byte(decl)))
		out.WriteString("\n\n")
	}
	fmt.Print(out.String())
	return nil
}

// normalize flattens a function to one canonical line and tightens it to the
// machine-minimal form. It is string-aware: `//` inside a string literal is not
// a comment, and whitespace inside string literals is preserved.
func normalize(src string) string {
	var parts []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(stripLineComment(line))
		if line != "" {
			parts = append(parts, line)
		}
	}
	return tighten(strings.Join(parts, " "))
}

// tightRe matches the whitespace around an operator/punctuation token, which the
// lexer does not need. The captured token is kept; the surrounding spaces drop.
// `^` (bitwise XOR / unary complement) is included: like the other single-char
// operators it never begins or ends a two-char token, so dropping the space
// around it is lossless — omitting it left `a ^ b` un-tightened, inconsistent
// with `a & b` / `a | b` and contrary to this form being the minimal one.
var tightRe = regexp.MustCompile(` *([(){}\[\],+\-*/%<>=!&|^;:]) *`)

// tighten removes insignificant whitespace from a normalized declaration: any
// space adjacent to an operator or punctuation token is dropped (the lexer only
// needs whitespace to separate two word tokens, e.g. `return n`). String
// literals are copied verbatim. This is the canonical machine form — measured at
// ~13% fewer agent tokens than spaced-out code (tools/tokmin.py), with zero
// semantic change.
func tighten(s string) string {
	var b strings.Builder
	seg := 0
	for i := 0; i < len(s); {
		if s[i] != '"' {
			i++
			continue
		}
		b.WriteString(tightRe.ReplaceAllString(s[seg:i], "$1")) // tighten code before the string
		j := i + 1                                              // copy the string literal verbatim
		for j < len(s) && s[j] != '"' {
			if s[j] == '\\' {
				j++
			}
			j++
		}
		if j < len(s) {
			j++ // include the closing quote
		}
		b.WriteString(s[i:j])
		i, seg = j, j
	}
	b.WriteString(tightRe.ReplaceAllString(s[seg:], "$1"))
	return b.String()
}

// stripLineComment removes a // comment from a line, ignoring // that appears
// inside a string literal.
func stripLineComment(line string) string {
	inStr := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inStr {
			if c == '\\' {
				i++ // skip escaped char
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		} else if c == '/' && i+1 < len(line) && line[i+1] == '/' {
			return line[:i]
		}
	}
	return line
}

// splitFunctions splits readable source into per-function blocks (brace-aware).
func splitFunctions(src string) ([]string, error) {
	var funcs []string
	var cur strings.Builder
	depth := 0
	started := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !started {
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			started = true
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		// Count braces, but skip those inside string literals or after a // comment
		// (a JSON-building function has "{"/"}" in strings — they are not blocks).
		inStr := false
		for i := 0; i < len(line); i++ {
			c := line[i]
			if inStr {
				if c == '\\' {
					i++
				} else if c == '"' {
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '/':
				if i+1 < len(line) && line[i+1] == '/' {
					i = len(line) // rest of the line is a comment
				}
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		// A block is complete when braces balance and it has a body ("{"), OR it is a
		// brace-less top-level `var` declaration (a package global), which is a single
		// logical line with no body to wait for.
		body := strings.TrimSpace(cur.String())
		if started && depth == 0 && (strings.Contains(body, "{") || strings.HasPrefix(body, "var ")) {
			funcs = append(funcs, body)
			cur.Reset()
			started = false
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		return nil, fmt.Errorf("unbalanced braces near: %s", strings.TrimSpace(cur.String()))
	}
	return funcs, nil
}
