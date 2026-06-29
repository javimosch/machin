package main

import (
	"fmt"
	"os"
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
