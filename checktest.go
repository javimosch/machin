package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// cmdCheckTest is the stage-3 (typecheck) oracle: run the Go checker on a whole
// program and dump, for every monomorphized instance, the inferred Kind of each
// param / named return / local. The MFL checker (selfhost/check.src) emits the
// identical form and the two are diffed.
//
//	machin checktest --program <file.mfl>
//
// Instances are keyed by "<source-name>|<signature>" and sorted, so the dump is
// deterministic and does not depend on the instantiation counter (which the MFL
// port need not reproduce exactly).
func cmdCheckTest(args []string) error {
	if len(args) >= 2 && args[0] == "--program" {
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
		c, err := Check(prog)
		if err != nil {
			fmt.Println("(check-error)")
			return nil
		}
		type row struct{ key, line string }
		var rows []row
		for _, inst := range c.Reps() {
			src := c.SrcFunc(inst)
			var b strings.Builder
			b.WriteString("(func " + src.Name)
			for i, p := range src.Params {
				b.WriteString(" (param " + p + " " + c.ParamKind(inst, i).String() + ")")
			}
			for i := 0; i < c.RetArity(inst); i++ {
				b.WriteString(" (ret " + strconv.Itoa(i) + " " + c.RetKindAt(inst, i).String() + ")")
			}
			for _, l := range c.Locals(inst) {
				b.WriteString(" (local " + l + " " + c.VarKind(inst, l).String() + ")")
			}
			b.WriteString(")")
			rows = append(rows, row{src.Name + "|" + c.sigString(inst), b.String()})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
		var out strings.Builder
		for _, r := range rows {
			out.WriteString(r.line + "\n")
		}
		fmt.Print(out.String())
		return nil
	}
	return fmt.Errorf("usage: machin checktest --program <file.mfl>")
}
