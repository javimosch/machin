// machin — the MFL (Machine-First Language) compiler.
//
// MFL is a backend language based on Go but machine-first: a program IS base64,
// one function per line, a blank line between functions. The human states
// intent; the machine reads and writes the code. There is no readable source of
// truth and no "decode" — the .mfl is the program. MFL is statically typed (by
// inference) and compiles to native code through C, so it runs at C/Rust/Zig
// speed for scalar work.
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
  machin build <file.mfl> --emit-c   print the generated C and stop
  machin encode <src>                mint MFL from loose Go-like text (machine tool)

A .mfl program is base64, one function per line, blank line between functions.
`)
}

// loadMFL reads a .mfl file, base64-decodes each non-blank line, and parses the
// decoded declarations into a Program (struct types + functions).
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
		raw, err := base64.StdEncoding.DecodeString(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: base64 decode: %w", n+1, err)
		}
		decls = append(decls, string(raw))
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

func cmdRun(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("run: need exactly one .mfl file")
	}
	prog, err := loadMFL(args[0])
	if err != nil {
		return err
	}
	bin, err := os.CreateTemp("", "mfl-run-*")
	if err != nil {
		return err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name()); err != nil {
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
	var src, out string
	emitC := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				return fmt.Errorf("build: -o needs a path")
			}
			i++
			out = args[i]
		case "--emit-c":
			emitC = true
		default:
			src = args[i]
		}
	}
	if src == "" {
		return fmt.Errorf("build: need a .mfl file")
	}
	prog, err := loadMFL(src)
	if err != nil {
		return err
	}
	if emitC {
		c, err := CompileToC(prog)
		if err != nil {
			return err
		}
		fmt.Print(c)
		return nil
	}
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	}
	if err := BuildBinary(prog, out); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "built %s\n", out)
	return nil
}

// cmdEncode lifts loose Go-like text into canonical MFL. Multiple source files
// are concatenated in order, so a framework can be composed with an app:
//   machin encode framework/machweb.src myapp.src > app.mfl
func cmdEncode(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("encode: need at least one source file")
	}
	var combined strings.Builder
	for _, path := range args {
		data, err := os.ReadFile(path)
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
		out.WriteString(base64.StdEncoding.EncodeToString([]byte(n)))
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

// normalize flattens a function to one canonical line, joining lines with a
// single space. It is string-aware: `//` inside a string literal is not a
// comment, and whitespace inside string literals is preserved.
func normalize(src string) string {
	var parts []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(stripLineComment(line))
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
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
