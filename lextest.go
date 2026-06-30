package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// cmdLexTest is the self-hosting oracle: lex a source file with the Go lexer and
// print the token stream in a canonical, escaping-free form — one token per line
// as "<kind> <pos> <hex(val)>". The MFL lexer (selfhost/lex.src) emits the exact
// same format, so the two are diffed byte-for-byte over the .src corpus. Hex on the
// value sidesteps every newline/quote/UTF-8 escaping mismatch between Go and MFL.
func cmdLexTest(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: machin lextest <file>")
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	toks, err := Lex(string(src))
	if err != nil {
		return err
	}
	var b []byte
	for _, t := range toks {
		b = append(b, fmt.Sprintf("%d %d %x\n", int(t.Kind), t.Pos, t.Val)...)
	}
	os.Stdout.Write(b)
	return nil
}

// cmdLexBench mirrors selfhost/lexbench.src: lex one file N times in-memory and
// report total tokens + elapsed ms. Same workload as the MFL lexer benchmark, so
// the two numbers are directly comparable (Go-compiled vs MFL-compiled, same algo).
func cmdLexBench(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: machin lexbench <file> <iters>")
	}
	src, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	iters, _ := strconv.Atoi(args[1])
	s := string(src)
	warm, err := Lex(s)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "tokens/iter: %d\n", len(warm))
	total := 0
	t0 := time.Now()
	for i := 0; i < iters; i++ {
		toks, _ := Lex(s)
		total += len(toks)
	}
	ms := time.Since(t0).Milliseconds()
	fmt.Printf("iters=%d total_tokens=%d elapsed_ms=%d\n", iters, total, ms)
	return nil
}
