package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
)

// cmdArenaTest is the self-hosting oracle for the compile-time arena escape analysis:
//
//	machin arenatest --program <file.mfl>
//
// It runs parse → liftClosures → check → detectArenaEscapes and dumps every ARENA00x
// finding in a canonical, escaping-free form (one per line, fields hex-encoded, sorted
// by function then detail) so the MFL port (selfhost/arena.src) can be byte-diffed.
func cmdArenaTest(args []string) error {
	var path string
	for _, a := range args {
		if a != "--program" {
			path = a
		}
	}
	if path == "" {
		return fmt.Errorf("usage: machin arenatest --program <file.mfl>")
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
	fs := detectArenaEscapes(prog, c)
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].Decl != fs[j].Decl {
			return fs[i].Decl < fs[j].Decl
		}
		return fs[i].Detail < fs[j].Detail
	})
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(hx(f.Decl) + "|" + hx(f.Code) + "|" + hx(f.Detail) + "\n")
	}
	os.Stdout.WriteString(b.String())
	return nil
}
