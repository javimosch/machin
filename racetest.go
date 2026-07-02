package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// cmdRaceTest is the self-hosting oracle for the data-race pass:
//
//	machin racetest --program <file.mfl>
//
// It runs parse → liftClosures → check → detectRaces and dumps every finding in a
// canonical, escaping-free form (one per line, fields hex-encoded, in detectRaces'
// stable sort order) so the MFL port can be byte-diffed against it.
func cmdRaceTest(args []string) error {
	if len(args) < 2 || args[0] != "--program" {
		return fmt.Errorf("usage: machin racetest --program <file.mfl>")
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
	var b strings.Builder
	for _, f := range detectRaces(prog, c) {
		hx := func(s string) string { return hex.EncodeToString([]byte(s)) }
		ws := make([]string, len(f.Writers))
		for i, w := range f.Writers {
			ws[i] = hx(w)
		}
		b.WriteString(hx(f.Decl) + "|" + hx(f.Kind) + "|" + hx(f.Root) + "|" + strings.Join(ws, ",") + "\n")
	}
	os.Stdout.WriteString(b.String())
	return nil
}
