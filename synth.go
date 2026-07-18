package main

// synth.go — agent/search-driven rewrite discovery: `machin superopt <fn> <file>`.
//
// Where `machin optimize` applies a FIXED catalog of peephole rules, this SEARCHES for a
// cheaper equivalent from scratch — bottom-up enumerative synthesis over a small arithmetic
// grammar, pruned by observational equivalence (only one representative per distinct behavior
// survives to build larger terms). A candidate whose behavior matches the target over a dense
// sampling domain is a hit; the cheapest hit (by the cost model) wins. This is where the big,
// backend-independent wins live: it can DISCOVER a closed form and eliminate a loop entirely —
// e.g. sum_loop(n) { for i<=n { acc+=i } }  ->  n * (n + 1) / 2.
//
// Honesty — synthesis from examples can OVERFIT (an expression that matches on the sampled
// points but differs elsewhere). Two guards: (1) the sampling domain is dense (ints -6..6),
// far more constraining than a handful of points; (2) the winner is re-checked over a WIDER
// domain (-20..20) and discarded if it disagrees. The reported verdict is still bounded
// ("equivalent over N inputs"), never "equivalent for all inputs" — a synthesized rewrite is a
// strong, evidence-backed suggestion to review, not a structural proof. The final result is
// also run through the same equivalence oracle as `equiv`/`optimize` for the official verdict.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// synthGrammar returns the constant and operator alphabet for a function of np parameters.
//
// Shifts are included (matching the interp's exact `l << uint(r)` semantics, so the fast evalInt
// and the certifying interpreter agree bit-for-bit — the same x*2^k → x<<k class `optimize`
// already ships), and the constant set is wide, so one-parameter search reaches clean forms like
// `a << 3` for an 8x multiply. Two-parameter search combines over a squared domain, so its
// enumeration blows up under that richer alphabet before reaching useful depth; it uses a leaner
// grammar that keeps the search tractable (the same alphabet the original engine shipped with).
func synthGrammar(np int) (consts []int64, ops []string) {
	if np <= 1 {
		return []int64{-2, -1, 0, 1, 2, 3, 4}, []string{"+", "-", "*", "/", "%", "<<", ">>"}
	}
	return []int64{-1, 0, 1, 2}, []string{"+", "-", "*", "/", "%"}
}

const (
	synthMaxCost    = 12     // max latency-cost of a candidate (a n*(n+1)/2-shaped form is 10)
	synthExploreCap = 300000 // max candidate terms generated
)

type synthResult struct {
	Fn         string `json:"fn"`
	Found      bool   `json:"found"`
	Expr       string `json:"expr,omitempty"` // rendered replacement expression
	Optimized  string `json:"optimized,omitempty"`
	Verdict    string `json:"verdict,omitempty"` // oracle verdict (equivalent / equivalent-bounded)
	CostBefore int    `json:"costBefore,omitempty"`
	CostAfter  int    `json:"costAfter,omitempty"`
	Explored   int    `json:"explored"`   // candidate terms generated
	Distinct   int    `json:"distinct"`   // distinct behaviors kept
	WidePoints int    `json:"widePoints"` // points the winner was re-verified on
	Note       string `json:"note,omitempty"`
}

// synthDomains builds the dense sampling domain (search) and the wider verification domain, one
// value list per int parameter. Returns ok=false if the function isn't int-only or has too many
// params for the search to stay bounded.
func synthDomains(np int) (sample, wide [][]int64, ok bool) {
	switch np {
	case 1:
		return [][]int64{iota64(-6, 6)}, [][]int64{iota64(-20, 20)}, true
	case 2:
		return [][]int64{iota64(-4, 4), iota64(-4, 4)}, [][]int64{iota64(-8, 8), iota64(-8, 8)}, true
	default:
		return nil, nil, false // 0 params: nothing to search over; >2: space explodes
	}
}

func iota64(lo, hi int64) []int64 {
	out := make([]int64, 0, hi-lo+1)
	for v := lo; v <= hi; v++ {
		out = append(out, v)
	}
	return out
}

// evalInt evaluates an int-only arithmetic candidate; ok=false on divide/modulo by zero.
func evalInt(e Expr, env []int64, pidx map[string]int) (int64, bool) {
	switch x := e.(type) {
	case *IntLit:
		return x.Val, true
	case *Ident:
		return env[pidx[x.Name]], true
	case *Unary:
		v, ok := evalInt(x.X, env, pidx)
		if !ok {
			return 0, false
		}
		if x.Op == "-" {
			return -v, true
		}
		return v, true
	case *Binary:
		l, lok := evalInt(x.L, env, pidx)
		r, rok := evalInt(x.R, env, pidx)
		if !lok || !rok {
			return 0, false
		}
		switch x.Op {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r == 0 {
				return 0, false
			}
			return l / r, true
		case "%":
			if r == 0 {
				return 0, false
			}
			return l % r, true
		case "<<":
			return l << uint(r), true // verbatim the interp (falsify.go evalBinary): Go shift semantics
		case ">>":
			return l >> uint(r), true
		}
	}
	return 0, false
}

// signature evaluates a candidate over every tuple of the domain, returning a behavior key
// (ok=false if the candidate traps anywhere — it can't be a total replacement).
func signature(e Expr, domain [][]int64, pidx map[string]int) (string, bool) {
	var b strings.Builder
	ok := forEachTuple(domain, func(env []int64) bool {
		v, good := evalInt(e, env, pidx)
		if !good {
			return false
		}
		b.WriteString(strconv.FormatInt(v, 10))
		b.WriteByte(',')
		return true
	})
	if !ok {
		return "", false
	}
	return b.String(), true
}

// targetSignature computes the target's behavior over the domain via the real interpreter.
// ok=false if any output is not a plain int (synthesis is int-only in this version).
func targetSignature(target *FuncDecl, domain [][]int64, funcs map[string]*FuncDecl, structs map[string]*TypeDecl) (string, bool) {
	var b strings.Builder
	ok := forEachTuple(domain, func(env []int64) bool {
		args := make([]fval, len(env))
		for i, v := range env {
			args[i] = vint(v)
		}
		out, conclusive := certEval(target, args, funcs, structs)
		if !conclusive || out.k != KInt {
			return false
		}
		b.WriteString(strconv.FormatInt(out.i, 10))
		b.WriteByte(',')
		return true
	})
	if !ok {
		return "", false
	}
	return b.String(), true
}

// forEachTuple invokes fn on every mixed-radix tuple of the domain; stops early (returns false)
// if fn returns false.
func forEachTuple(domain [][]int64, fn func(env []int64) bool) bool {
	idx := make([]int, len(domain))
	env := make([]int64, len(domain))
	for {
		for i := range domain {
			env[i] = domain[i][idx[i]]
		}
		if !fn(env) {
			return false
		}
		k := 0
		for ; k < len(idx); k++ {
			idx[k]++
			if idx[k] < len(domain[k]) {
				break
			}
			idx[k] = 0
		}
		if k == len(idx) {
			return true
		}
	}
}

// synthesize searches for the cheapest arithmetic expression whose behavior matches target over
// the sample domain and survives verification over the wide domain.
func synthesize(target *FuncDecl, params []string, funcs map[string]*FuncDecl, structs map[string]*TypeDecl) synthResult {
	res := synthResult{Fn: target.Name}
	sample, wide, ok := synthDomains(len(params))
	if !ok {
		res.Note = "synthesis supports 1–2 int parameters"
		return res
	}
	pidx := map[string]int{}
	for i, p := range params {
		pidx[p] = i
	}
	targetSig, ok := targetSignature(target, sample, funcs, structs)
	if !ok {
		res.Note = "target is not an int-only function (synthesis is int→int in this version)"
		return res
	}
	targetWide, _ := targetSignature(target, wide, funcs, structs)
	consts, ops := synthGrammar(len(params))

	// Bottom-up enumeration in COST order with observational-equivalence pruning. Terms are
	// generated by increasing total cost (the same latency model the report uses), so the FIRST
	// term realising a given behavior is its cost-minimum — dedup then keeps the cheapest
	// representative, not merely the smallest. (Enumerating by node-size instead would keep
	// `b*3` over the cheaper `b+b+b`, since the multiply is smaller though costlier.)
	bank := map[int][]Expr{} // cost -> representative terms (cheapest per distinct behavior)
	seen := map[string]bool{}
	var best Expr
	bestCost := funcCost(target) // only a STRICTLY cheaper equivalent replaces the original

	// add registers a term of the given cost; it becomes a representative iff its behavior is new
	// (which, in cost order, means this is the cheapest way seen to realise that behavior).
	add := func(e Expr, cost int) {
		if res.Explored >= synthExploreCap {
			return
		}
		res.Explored++
		sig, good := signature(e, sample, pidx)
		if !good {
			return
		}
		if sig == targetSig && cost < bestCost {
			// guard against overfitting: the winner must also match on the wide domain
			if wsig, _ := signature(e, wide, pidx); wsig == targetWide {
				best, bestCost = e, cost
			}
		}
		if seen[sig] {
			return
		}
		seen[sig] = true
		bank[cost] = append(bank[cost], e)
	}

	// cost 0: parameters and constants (leaves are free in the latency model)
	for _, p := range params {
		add(&Ident{Name: p}, 0)
	}
	for _, k := range consts {
		add(&IntLit{Val: k}, 0)
	}

	// Enumerate cost levels while a strictly-cheaper term is still possible. Because terms are
	// generated in cost order and only a term with cost < bestCost can improve the answer, once
	// c reaches bestCost nothing cheaper remains — so the loop stops. bestCost tightens as better
	// matches are found (starting at the original's cost), which both proves the winner optimal
	// and instantly settles the "already minimal" case (nothing below the original's own cost).
	for c := 1; c <= synthMaxCost && c < bestCost && res.Explored < synthExploreCap; c++ {
		// unary: -a costs 1 + cost(a)
		for _, a := range bank[c-1] {
			add(&Unary{Op: "-", X: a}, c)
		}
		// binary: a op b costs cost(a) + opCost(op) + cost(b) == c
		for _, op := range ops {
			oc := opCost(op)
			for i := 0; i <= c-oc; i++ {
				j := c - oc - i
				if j < 0 {
					continue
				}
				for _, a := range bank[i] {
					for _, b := range bank[j] {
						add(&Binary{Op: op, L: a, R: b}, c)
						if res.Explored >= synthExploreCap {
							break
						}
					}
				}
			}
		}
	}

	res.Distinct = len(seen)
	if best == nil {
		res.Note = "no cheaper equivalent found within the search bounds"
		return res
	}
	res.Found = true
	res.WidePoints = tupleCount(wide)
	res.Expr = exprToStr(best)
	res.CostBefore = funcCost(target)
	res.CostAfter = exprCost(best)
	return res
}

func tupleCount(domain [][]int64) int {
	n := 1
	for _, d := range domain {
		n *= len(d)
	}
	return n
}

func exprToStr(e Expr) string {
	r := &renderer{complete: true}
	return r.expr(e, 0)
}

// cmdSuperopt implements `machin superopt <fn> <file...>`.
func cmdSuperopt(args []string) error {
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
	if len(names) != 1 || len(files) == 0 {
		return fmt.Errorf("usage: machin superopt <fn> [--json] <file...> — search for a cheaper equivalent of <fn>")
	}
	var combined strings.Builder
	for _, p := range files {
		data, err := readModule(p)
		if err != nil {
			return fmt.Errorf("superopt: %w", err)
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}
	blocks, _, err := splitFunctionsLoc(combined.String())
	if err != nil {
		return fmt.Errorf("superopt: %w", err)
	}
	decls := make([]string, len(blocks))
	for i, b := range blocks {
		decls[i] = normalize(b)
	}
	prog, perr := ParseProgram(decls)
	if perr != nil {
		return fmt.Errorf("superopt: parse: %w", perr)
	}
	c, cerr := Check(prog)
	if cerr != nil {
		return fmt.Errorf("superopt: %w (superopt needs a program that typechecks)", cerr)
	}

	funcs := map[string]*FuncDecl{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	// find the instantiated target
	var target *FuncDecl
	for _, inst := range c.reps {
		if fn := c.instFn[inst]; fn != nil && fn.Name == names[0] {
			target = fn
			break
		}
	}
	if target == nil {
		return fmt.Errorf("superopt: function %q is not defined or not reachable (only compiled functions can be searched)", names[0])
	}

	res := synthesize(target, target.Params, funcs, c.structs)

	// certify the winner through the same oracle as equiv/optimize, and build the optimized func
	if res.Found {
		cand := &FuncDecl{Name: target.Name + "$syn", Params: target.Params, Returns: []string{"__r"},
			Body: []Stmt{&ReturnStmt{Vals: []Expr{mustParseExpr(res.Expr)}}}}
		if dom, prov, ok := paramDomainsFor(c, names[0], len(target.Params)); ok {
			v := equivOverDomains(target.Name, cand.Name, target, cand, dom, prov, funcs, c.structs)
			res.Verdict = v.Verdict
			if v.Verdict == "diverges" { // should not happen (wide-checked), but stay honest
				res.Found = false
				res.Note = "candidate failed oracle certification: " + v.Bind
			}
		}
		if res.Found {
			opt := &FuncDecl{Name: target.Name, Params: target.Params, Returns: target.Returns}
			ret := target.Returns
			if len(ret) == 1 {
				opt.Body = []Stmt{&AssignStmt{Name: ret[0], Op: "=", Val: mustParseExpr(res.Expr)}}
			} else {
				opt.Body = []Stmt{&ReturnStmt{Vals: []Expr{mustParseExpr(res.Expr)}}}
			}
			if src, complete := renderFuncSrc(opt); complete {
				res.Optimized = src
			}
		}
	}

	if jsonOut {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
	} else {
		printSynthResult(res)
	}
	return nil
}

// paramDomainsFor gathers the oracle's bounded domain for the named function.
func paramDomainsFor(c *Checker, name string, np int) ([][]fval, int, bool) {
	for _, inst := range c.reps {
		if fn := c.instFn[inst]; fn != nil && fn.Name == name {
			return paramDomains(c, inst, np)
		}
	}
	return nil, 0, false
}

// mustParseExpr parses a single expression by wrapping it in a throwaway function.
func mustParseExpr(src string) Expr {
	prog, err := ParseProgram([]string{normalize("func __e() (r) { r = " + src + " }")})
	if err != nil || len(prog.Funcs) == 0 {
		return &IntLit{Val: 0}
	}
	for _, s := range prog.Funcs[0].Body {
		if a, ok := s.(*AssignStmt); ok {
			return a.Val
		}
	}
	return &IntLit{Val: 0}
}

func printSynthResult(r synthResult) {
	fmt.Printf("  searching for a cheaper equivalent of %s...\n", r.Fn)
	if !r.Found {
		note := r.Note
		if note == "" {
			note = "no cheaper equivalent found within the search bounds"
		}
		fmt.Printf("  %s  (explored %d candidates, %d distinct behaviors)\n", note, r.Explored, r.Distinct)
		return
	}
	fmt.Printf("  found: %s\n", r.Expr)
	fmt.Printf("    oracle verdict: %s\n", r.Verdict)
	fmt.Printf("    cost %d → %d  (−%d%%, static op-latency estimate)\n", r.CostBefore, r.CostAfter, costPct(r.CostBefore, r.CostAfter))
	fmt.Printf("    matched the original on every one of %d wider inputs (−20..20) before accepting — synthesized, so review before relying on it\n", r.WidePoints)
	fmt.Printf("    (explored %d candidates, %d distinct behaviors)\n", r.Explored, r.Distinct)
	if r.Optimized != "" {
		fmt.Printf("\n  %s can be replaced by:  %s\n", r.Fn, r.Optimized)
	}
}
