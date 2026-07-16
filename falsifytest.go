package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// cmdFalsifyTest is the self-hosting oracle for the falsify pass:
//
//	machin falsifytest --program <file.mfl>
//
// It runs parse → liftClosures → check → detectFalsifiable and dumps every
// counterexample in a canonical, escaping-free form (one per line, fields
// hex-encoded, in detectFalsifiable's stable sort order) so the MFL port
// (selfhost/falsify.src) can be byte-diffed against it.
func cmdFalsifyTest(args []string) error {
	if len(args) < 2 || args[0] != "--program" {
		return fmt.Errorf("usage: machin falsifytest --program <file.mfl>")
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
	hx := func(s string) string { return hex.EncodeToString([]byte(s)) }
	var b strings.Builder
	for _, f := range detectFalsifiable(prog, c) {
		b.WriteString(hx(f.Decl) + "|" + hx(f.Code) + "|" + hx(f.Prop) + "|" + hx(f.Expr) + "|" + hx(f.Bind) + "\n")
	}
	os.Stdout.WriteString(b.String())
	return nil
}
