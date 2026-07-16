package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"sort"
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
	prove := false
	var path string
	for _, a := range args {
		if a == "--prove" {
			prove = true
		} else if a != "--program" {
			path = a
		}
	}
	if path == "" {
		return fmt.Errorf("usage: machin falsifytest --program <file.mfl> [--prove]")
	}
	data, err := os.ReadFile(path)
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
	if prove {
		// prove mode: dump the per-function verdict (fn|verdict), sorted by fn — the
		// key output of --prove. Dense-enumeration findings are implied by the
		// `counterexample` verdicts (the enumeration machinery is the non-prove gate).
		falsProve = true
		_, _, verdicts := detectFalsifiableStats(prog, c)
		sort.Slice(verdicts, func(i, j int) bool { return verdicts[i].Fn < verdicts[j].Fn })
		for _, v := range verdicts {
			b.WriteString(hx(v.Fn) + "|" + hx(v.Verdict) + "\n")
		}
		falsProve = false
	} else {
		for _, f := range detectFalsifiable(prog, c) {
			b.WriteString(hx(f.Decl) + "|" + hx(f.Code) + "|" + hx(f.Prop) + "|" + hx(f.Expr) + "|" + hx(f.Bind) + "\n")
		}
	}
	os.Stdout.WriteString(b.String())
	return nil
}
