package main

// equiv.go — the bounded equivalence oracle: prove two functions produce identical output
// over the Falsifier's bounded input space, or return the exact input where they diverge.
//
// This is the verification step behind provable superoptimization (accept a rewrite only if
// it's proven equivalent) and a standalone "did my refactor change behavior?" tool. It runs
// both functions through machin's own interpreter (the same evaluator certify and the
// Falsifier use) over the shared bounded input domain and compares canonical outputs.
//
// Honest, Falsifier-style: `equivalent` over a finite domain is a total result; over a
// bounded int/slice domain it is `equivalent-bounded` (proven only up to the bounds); a
// single divergence is a hard counterexample; if nothing can be conclusively compared, it is
// `inconclusive` (never silently "equivalent").

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type equivResult struct {
	F        string `json:"f"`
	G        string `json:"g"`
	Verdict  string `json:"verdict"` // equivalent | equivalent-bounded | diverges | inconclusive
	Tried    int    `json:"tried"`
	Compared int    `json:"compared"`
	// on "diverges":
	Bind string `json:"input,omitempty"`
	FVal string `json:"fValue,omitempty"`
	GVal string `json:"gValue,omitempty"`
}

// checkEquiv enumerates fName's bounded input domain and compares fName vs gName on every
// tuple (both evaluated by the interpreter). Same param types ⇒ the same domain applies to
// both, so one enumeration drives the comparison.
func checkEquiv(prog *Program, c *Checker, fName, gName string) equivResult {
	res := equivResult{F: fName, G: gName, Verdict: "inconclusive"}
	funcs := map[string]*FuncDecl{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	structs := c.structs

	var fInst string
	var fFn *FuncDecl
	for _, inst := range c.reps {
		if fn := c.instFn[inst]; fn != nil && fn.Name == fName {
			fInst, fFn = inst, fn
			break
		}
	}
	gFn := funcs[gName]
	if fFn == nil || gFn == nil || len(fFn.Params) != len(gFn.Params) {
		return res // arity mismatch or not instantiated → inconclusive
	}

	slots := c.funcParam[fInst]
	domains := make([][]fval, len(fFn.Params))
	prov := 2
	for i, slot := range slots {
		d, ok := c.domain(slot)
		if !ok {
			return res // an unbounded param domain — can't enumerate
		}
		domains[i] = d
		if p := c.provability(slot); p < prov {
			prov = p
		}
	}

	idx := make([]int, len(domains))
	tried, compared, anyUnknown, completed := 0, 0, false, false
	for {
		if tried >= falsInputCap {
			break
		}
		args := make([]fval, len(domains))
		for i := range domains {
			args[i] = domains[i][idx[i]]
		}
		tried++
		fv, fok := certEval(fFn, args, funcs, structs)
		gv, gok := certEval(gFn, args, funcs, structs)
		if fok && gok {
			fs, fr := renderCanonical(fv)
			gs, gr := renderCanonical(gv)
			if fr && gr {
				compared++
				if fs != gs {
					res.Verdict, res.Tried, res.Compared = "diverges", tried, compared
					res.Bind, res.FVal, res.GVal = bindArgList(args), fs, gs
					return res
				}
			} else {
				anyUnknown = true
			}
		} else {
			anyUnknown = true
		}
		k := 0
		for ; k < len(idx); k++ {
			idx[k]++
			if idx[k] < len(domains[k]) {
				break
			}
			idx[k] = 0
		}
		if k == len(idx) {
			completed = true
			break
		}
	}

	res.Tried, res.Compared = tried, compared
	switch {
	case compared == 0:
		res.Verdict = "inconclusive"
	case anyUnknown || !completed:
		res.Verdict = "equivalent-bounded" // agreed on the modeled inputs / cap-truncated
	case prov == 2:
		res.Verdict = "equivalent" // finite domain, whole space, all agreed — total
	default:
		res.Verdict = "equivalent-bounded" // agreed, but over a bounded int/slice domain
	}
	return res
}

// cmdEquiv implements `machin equiv <f> <g> <file...>`.
func cmdEquiv(args []string) error {
	jsonOut := false
	var names, files []string
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else if strings.HasSuffix(a, ".mfl") || fileExists(a) {
			files = append(files, a)
		} else {
			names = append(names, a)
		}
	}
	if len(names) != 2 || len(files) == 0 {
		return fmt.Errorf("usage: machin equiv <f> <g> <file...> — prove two functions are equivalent")
	}
	var combined strings.Builder
	for _, p := range files {
		data, err := readModule(p)
		if err != nil {
			return fmt.Errorf("equiv: %w", err)
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}
	blocks, _, err := splitFunctionsLoc(combined.String())
	if err != nil {
		return fmt.Errorf("equiv: %w", err)
	}
	decls := make([]string, len(blocks))
	for i, b := range blocks {
		decls[i] = normalize(b)
	}
	prog, perr := ParseProgram(decls)
	if perr != nil {
		return fmt.Errorf("equiv: parse: %w", perr)
	}
	c, cerr := Check(prog)
	if cerr != nil {
		return fmt.Errorf("equiv: %w (equiv needs a program that typechecks)", cerr)
	}

	res := checkEquiv(prog, c, names[0], names[1])
	if jsonOut {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
	} else {
		printEquiv(res)
	}
	if res.Verdict == "diverges" {
		os.Exit(1)
	}
	return nil
}

func printEquiv(r equivResult) {
	switch r.Verdict {
	case "equivalent":
		fmt.Printf("equivalent: %s and %s produce identical output on every input (finite domain, %d checked)\n", r.F, r.G, r.Compared)
	case "equivalent-bounded":
		fmt.Printf("equivalent-bounded: %s and %s agree on all %d checked inputs, up to the bounds (ints %v, slice len ≤ %d) — not a total proof\n",
			r.F, r.G, r.Compared, falsIntDomain, falsSliceLenMax)
	case "diverges":
		fmt.Printf("DIVERGES: %s(%s) = %s but %s(%s) = %s — the two functions are NOT equivalent\n", r.F, r.Bind, r.FVal, r.G, r.Bind, r.GVal)
	default:
		fmt.Printf("inconclusive: could not compare %s and %s (mismatched shape, or a domain/body the interpreter can't model)\n", r.F, r.G)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
