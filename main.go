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
	case "run":
		err = cmdRun(os.Args[2:])
	case "build":
		err = cmdBuild(os.Args[2:])
	case "encode":
		err = cmdEncode(os.Args[2:])
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

// loadMFL reads a .mfl file — one declaration per non-blank line — and parses
// it into a Program (struct types + functions). The canonical form is plain
// text; packed (base64) lines are accepted too (see declFromLine).
func loadMFL(path string) (*Program, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var decls []string
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		decl, err := declFromLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, n+1, err)
		}
		decls = append(decls, decl)
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

func cmdRun(args []string) error {
	safe := false
	var src string
	for _, a := range args {
		if a == "--safe" {
			safe = true
		} else {
			src = a
		}
	}
	if src == "" {
		return fmt.Errorf("run: need exactly one .mfl file")
	}
	prog, err := loadMFL(src)
	if err != nil {
		return err
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
	emitC, safe, static := false, false, false
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
//   machin encode framework/machweb.src myapp.src > app.mfl
func cmdEncode(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("encode: need at least one source file")
	}
	var combined strings.Builder
	for _, path := range args {
		data, err := readModule(path) // local file, else an embedded framework module
		if err != nil {
			return err
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}
	blocks, err := splitFunctions(combined.String())
	if err != nil {
		return err
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
		return fmt.Errorf("parse: %w", err)
	}
	if _, err := Check(prog); err != nil {
		return fmt.Errorf("typecheck: %w", err)
	}
	fmt.Print(out.String())
	return nil
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
var tightRe = regexp.MustCompile(` *([(){}\[\],+\-*/%<>=!&|;:]) *`)

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
