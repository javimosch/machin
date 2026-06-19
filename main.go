// machin — the MFL (Machine-First Language) toolchain.
//
// MFL is a backend language based on Go but machine-first: a program is stored
// as base64. Each function lives on exactly one line, base64-encoded, with a
// blank line between functions. Humans author readable .mfs sources; the
// toolchain encodes them into the canonical machine-first .mfl form, decodes
// them back, and runs them directly.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	var err error
	switch cmd {
	case "run":
		err = cmdRun(os.Args[2:])
	case "encode":
		err = cmdEncode(os.Args[2:])
	case "decode":
		err = cmdDecode(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `machin — Machine-First Language toolchain

usage:
  machin run    <file.mfl>     decode + execute a machine-first program
  machin encode <file.mfs>     compile readable source -> machine-first .mfl (stdout)
  machin decode <file.mfl>     expand machine-first .mfl -> readable source (stdout)

A .mfl program is base64, one function per line, blank line between functions.
A .mfs source is the human-readable Go-like form of the same program.
`)
}

// splitFunctions splits readable source into per-function source blocks.
// Functions are delimited by top-level `func` keywords; brace-aware.
func splitFunctions(src string) ([]string, error) {
	var funcs []string
	var cur strings.Builder
	depth := 0
	started := false
	lines := strings.Split(src, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !started {
			if trimmed == "" || strings.HasPrefix(trimmed, "//") {
				continue
			}
			started = true
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		for _, c := range line {
			switch c {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		if started && depth == 0 && strings.Contains(cur.String(), "{") {
			funcs = append(funcs, strings.TrimSpace(cur.String()))
			cur.Reset()
			started = false
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		return nil, fmt.Errorf("unbalanced braces near: %s", strings.TrimSpace(cur.String()))
	}
	return funcs, nil
}

func cmdEncode(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("encode: need exactly one source file")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	funcs, err := splitFunctions(string(data))
	if err != nil {
		return err
	}
	var out strings.Builder
	for i, f := range funcs {
		// Validate it parses before emitting.
		if _, perr := ParseFunc(normalize(f)); perr != nil {
			return fmt.Errorf("function %d: %w", i+1, perr)
		}
		out.WriteString(base64.StdEncoding.EncodeToString([]byte(normalize(f))))
		out.WriteString("\n\n")
	}
	fmt.Print(out.String())
	return nil
}

// normalize collapses a multi-line function body to the canonical single-line
// machine form (whitespace-separated tokens are preserved by the lexer).
func normalize(src string) string {
	// Strip line comments and join lines with spaces.
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(strings.TrimSpace(line))
		b.WriteByte(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func cmdDecode(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("decode: need exactly one .mfl file")
	}
	fns, err := loadMFL(args[0])
	if err != nil {
		return err
	}
	for _, f := range fns {
		fmt.Println(prettyFunc(f))
		fmt.Println()
	}
	return nil
}

func cmdRun(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("run: need exactly one .mfl file")
	}
	fns, err := loadMFL(args[0])
	if err != nil {
		return err
	}
	in := NewInterp()
	for _, f := range fns {
		if err := in.Register(f); err != nil {
			return err
		}
	}
	out, err := in.Run()
	fmt.Print(out)
	return err
}

// loadMFL reads a .mfl file, base64-decodes each non-blank line, and parses
// each into a FuncDecl.
func loadMFL(path string) ([]*FuncDecl, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fns []*FuncDecl
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: base64 decode: %w", n+1, err)
		}
		fn, err := ParseFunc(string(raw))
		if err != nil {
			return nil, fmt.Errorf("line %d: parse: %w", n+1, err)
		}
		fns = append(fns, fn)
	}
	return fns, nil
}
