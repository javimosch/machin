package main

import (
	"fmt"
	"os"
	"strings"
)

// cmdCGenTest is the Stage-4 (C codegen) oracle: run the Go codegen on a whole
// program and emit ONLY the program-specific C — the static ~2000-line runtime
// prelude is skipped (bodyOnly), since both the Go and MFL codegens emit it
// identically. The MFL codegen (selfhost/cg*.src) emits the same body, diffed
// byte-for-byte.
//
//	machin cgentest --program <file.mfl>
func cmdCGenTest(args []string) error {
	if len(args) < 2 || args[0] != "--program" {
		return fmt.Errorf("usage: machin cgentest --program <file.mfl>")
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	var decls []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) != "" {
			decls = append(decls, ln)
		}
	}
	prog, err := ParseProgram(decls)
	if err != nil {
		fmt.Println("(parse-error)")
		return nil
	}
	liftClosures(prog)
	c, err := Check(prog)
	if err != nil {
		fmt.Println("(check-error)")
		return nil
	}
	// EVERY memo, or the first program to use the feature panics on a nil map
	// rather than emitting C. This path is what the codegen oracle runs, so a
	// missing one is a crash waiting for someone to add a program that uses
	// sort_by or copy to the corpus.
	g := &cgen{c: c, target: targetNative, bodyOnly: true,
		jsonMemo: map[string]string{}, parseMemo: map[string]string{},
		sortMemo: map[string]string{}, copyMemo: map[string]string{},
		chanJSONMemo: map[string][2]string{}}
	src, err := g.program(prog)
	if err != nil {
		fmt.Println("(codegen-error)")
		return nil
	}
	os.Stdout.WriteString(src)
	return nil
}
