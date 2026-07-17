package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
)

// cmdDeadlockTest is the self-hosting oracle for the compile-time deadlock finder:
//
//	machin deadlocktest --program <file.mfl>
//
// It runs parse → liftClosures → check → detectDeadlocks and dumps every DL001 finding
// in a canonical, escaping-free form (one per line, fields hex-encoded, sorted by
// function then channel) so the MFL port (selfhost/deadlock.src) can be byte-diffed.
func cmdDeadlockTest(args []string) error {
	var path string
	for _, a := range args {
		if a != "--program" {
			path = a
		}
	}
	if path == "" {
		return fmt.Errorf("usage: machin deadlocktest --program <file.mfl>")
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
	fs := detectDeadlocks(prog, c)
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Decl != fs[j].Decl {
			return fs[i].Decl < fs[j].Decl
		}
		return fs[i].Chan < fs[j].Chan
	})
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(hx(f.Decl) + "|" + hx(f.Code) + "|" + hx(f.Chan) + "|" + hx(f.Prop) + "\n")
	}
	os.Stdout.WriteString(b.String())
	return nil
}
