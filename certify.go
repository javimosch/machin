package main

// certify.go — translation validation, a.k.a. "the self-certifying compiler."
//
// Every other guarantee machin makes (race-freedom, falsification, replay) assumes
// the compiler faithfully implemented your source. This pass checks that assumption
// per build: it runs each function through machin's own concrete interpreter (the
// SOURCE semantics — the same evaluator the Falsifier uses) AND through the actually
// compiled binary (the CODEGEN semantics) over the same bounded input space, and
// confirms they produce identical results — or hands back the exact input where the
// compiler diverged from the source (a TRV001 miscompilation).
//
// Honest, Falsifier-style: a clean result means "no miscompilation found within the
// bounds," never "the compiler is proven correct." Unsound-complete — every reported
// divergence is a real discrepancy between source and codegen.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// cmdCertify implements `machin certify <file>` — the self-certifying compiler.
func cmdCertify(args []string) error {
	jsonOut := false
	var files []string
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("certify: need a source file")
	}
	var combined strings.Builder
	for _, p := range files {
		data, err := readModule(p)
		if err != nil {
			return fmt.Errorf("certify: %w", err)
		}
		combined.Write(data)
		combined.WriteByte('\n')
	}
	blocks, _, err := splitFunctionsLoc(combined.String())
	if err != nil {
		return fmt.Errorf("certify: %w", err)
	}
	decls := make([]string, len(blocks))
	for i, b := range blocks {
		decls[i] = normalize(b)
	}
	prog, perr := ParseProgram(decls)
	if perr != nil {
		return fmt.Errorf("certify: parse: %w", perr)
	}
	c, cerr := Check(prog)
	if cerr != nil {
		return fmt.Errorf("certify: %w (certify needs a program that typechecks)", cerr)
	}

	rep, err := certifyProgram(prog, c, decls)
	if err != nil {
		return err
	}

	if jsonOut {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		printCertReport(rep)
	}
	if !rep.OK {
		os.Exit(1) // a miscompilation is a hard failure
	}
	return nil
}

func printCertReport(rep certReport) {
	for _, v := range rep.Verdicts {
		switch v.Verdict {
		case "miscompiled":
			fmt.Printf("  MISCOMPILED  %s: %s = %s (source) but the compiler produced %s (tried %d)\n",
				v.Fn, v.Expr, v.Source, v.Compiled, v.Tried)
		case "certified":
			fmt.Printf("  certified          %s (%d inputs, whole space)\n", v.Fn, v.Tried)
		case "certified-bounded":
			fmt.Printf("  certified-bounded  %s (%d inputs, up to bounds)\n", v.Fn, v.Tried)
		case "partial":
			fmt.Printf("  partial            %s (%d inputs; some not modeled)\n", v.Fn, v.Tried)
		case "unknown":
			fmt.Printf("  unknown            %s (not validatable)\n", v.Fn)
		}
	}
	if rep.OK {
		fmt.Printf("certify: no miscompilation found — %d functions validated, %d skipped (within bounds: ints %v, slice len ≤ %d)\n",
			rep.Checked, rep.Skipped, rep.Bounds.IntDomain, rep.Bounds.SliceLenMax)
	} else {
		fmt.Printf("certify: MISCOMPILATION found — the compiler diverged from the source. See TRV above.\n")
	}
}

const certProbeCap = 20000 // max (function, input) probes compiled into one harness

// certVerdict is the per-function result.
type certVerdict struct {
	Fn      string `json:"fn"`
	Verdict string `json:"verdict"` // certified | certified-bounded | partial | unknown | miscompiled
	Tried   int    `json:"tried"`
	// populated only on "miscompiled":
	Expr     string `json:"expr,omitempty"`     // the failing call, e.g. mul(-3, -3)
	Source   string `json:"source,omitempty"`   // interpreter (source) result
	Compiled string `json:"compiled,omitempty"` // compiled-binary result
}

type certReport struct {
	OK       bool          `json:"ok"` // no miscompilation found
	Verdicts []certVerdict `json:"functions"`
	Checked  int           `json:"checked"`
	Skipped  int           `json:"skipped"`
	Bounds   falsifyBounds `json:"bounds"`
}

// a single (function, input tuple) validation probe.
type certProbe struct {
	fn     string
	args   []fval
	call   string // rendered call expression: fn(arg0, arg1, ...)
	source string // interpreter result, rendered as str() would
}

// certEval runs the interpreter on one tuple and returns the function's return value
// and whether the evaluation was conclusive (fully modeled, no trap, no filtered
// precondition). Mirrors runOne but yields the value instead of a trap verdict.
func certEval(fn *FuncDecl, args []fval, funcs map[string]*FuncDecl, structs map[string]*TypeDecl) (fval, bool) {
	ip := &interp{env: map[string]fval{}, funcs: funcs, structs: structs}
	for i, p := range fn.Params {
		if i < len(args) {
			ip.env[p] = args[i].clone()
		}
	}
	for _, r := range fn.Returns {
		ip.env[r] = vint(0)
	}
	for _, req := range fn.Requires {
		ok, cond := ip.evalPred(req)
		if !ok || !cond {
			return fval{}, false // inconclusive or precondition-filtered → not validated
		}
	}
	trap, unk := ip.runBody(fn.Body)
	if unk || trap != nil {
		return fval{}, false // unmodeled path, or an input that traps (would panic under --safe)
	}
	if ip.retValSet {
		return ip.retVal, true
	}
	if len(fn.Returns) == 1 {
		return ip.env[fn.Returns[0]], true
	}
	return fval{}, false
}

// renderCanonical renders a value EXACTLY as machin's json() builtin serializes it, so
// the interpreter's value and the compiled `json(...)` output compare as text across all
// data types (scalars, strings, slices, structs). Reports ok=false only for kinds json()
// can't serialize (closures/channels/maps) — those functions stay unknown, never certified.
//
// Float uses %.6g to match C's %g (mfl_str_d), NOT Go's default %g (shortest form) — a
// mismatch there would be a false miscompilation blamed on the compiler.
func renderCanonical(v fval) (string, bool) {
	switch v.k {
	case KInt:
		return fmt.Sprintf("%d", v.i), true
	case KFloat:
		return fmt.Sprintf("%.6g", v.f), true
	case KBool:
		if v.b {
			return "true", true
		}
		return "false", true
	case KString:
		return jsonEscape(v.s), true
	case KSlice:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range v.sl {
			if i > 0 {
				b.WriteByte(',')
			}
			s, ok := renderCanonical(e)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		b.WriteByte(']')
		return b.String(), true
	case KStruct:
		var b strings.Builder
		b.WriteByte('{')
		for i, f := range v.fieldOrder {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(jsonEscape(f))
			b.WriteByte(':')
			s, ok := renderCanonical(v.fields[f])
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		b.WriteByte('}')
		return b.String(), true
	}
	return "", false // closure / channel / map return — not json-serializable here
}

// jsonEscape quotes + escapes a string exactly like the runtime's mfl_json_str.
func jsonEscape(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// certifyProgram runs translation validation over every representative function.
// decls are the (normalized) source function strings, used to build the harness.
func certifyProgram(prog *Program, c *Checker, decls []string) (certReport, error) {
	funcs := map[string]*FuncDecl{}
	for _, f := range prog.Funcs {
		funcs[f.Name] = f
	}
	structs := c.structs

	var probes []certProbe
	vIndex := map[string]int{}
	var verdicts []certVerdict
	record := func(name, verdict string, tried int) {
		if i, ok := vIndex[name]; ok {
			verdicts[i].Tried += tried
			if certRank(verdict) > certRank(verdicts[i].Verdict) {
				verdicts[i].Verdict = verdict
			}
			return
		}
		vIndex[name] = len(verdicts)
		verdicts = append(verdicts, certVerdict{Fn: name, Verdict: verdict, Tried: tried})
	}

	rep := certReport{OK: true}

	for _, inst := range c.reps {
		fn := c.instFn[inst]
		if fn == nil || fn.Name == "main" {
			continue
		}
		if len(fn.Returns) != 1 {
			record(fn.Name, "unknown", 0) // no single return value to compare
			continue
		}
		slots := c.funcParam[inst]
		domains := make([][]fval, len(fn.Params))
		scoped := true
		prov := 2
		for i, slot := range slots {
			d, ok := c.domain(slot)
			if !ok {
				scoped = false
				break
			}
			domains[i] = d
			if p := c.provability(slot); p < prov {
				prov = p
			}
		}
		if !scoped {
			record(fn.Name, "unknown", 0)
			continue
		}

		// enumerate the bounded input space (mixed-radix over the per-param domains).
		idx := make([]int, len(domains))
		tried, conclusive := 0, 0
		anyUnknown, completed, unrenderable := false, false, false
		for {
			if tried >= falsInputCap {
				break
			}
			args := make([]fval, len(domains))
			for i := range domains {
				args[i] = domains[i][idx[i]]
			}
			tried++
			val, ok := certEval(fn, args, funcs, structs)
			if !ok {
				anyUnknown = true
			} else if s, sok := renderCanonical(val); !sok {
				unrenderable = true // a return json() can't serialize (closure/chan/map)
			} else {
				conclusive++
				probes = append(probes, certProbe{
					fn:     fn.Name,
					args:   args,
					call:   fmt.Sprintf("%s(%s)", fn.Name, bindArgList(args)),
					source: s,
				})
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

		switch {
		case conclusive == 0:
			record(fn.Name, "unknown", tried) // never conclusively evaluated / unserializable
		case anyUnknown || unrenderable || !completed:
			record(fn.Name, "partial", tried) // validated the conclusive inputs only
		case prov == 2:
			record(fn.Name, "certified", tried) // finite domain, whole space covered
		default:
			record(fn.Name, "certified-bounded", tried) // agreed up to the reported bounds
		}
	}

	if len(probes) > certProbeCap {
		probes = probes[:certProbeCap] // bound the harness size (reported via Skipped)
	}

	// Compile ONE harness that runs every probe's call and prints str(result), then
	// diff line-by-line against the interpreter's expected values.
	if len(probes) > 0 {
		got, err := runCertHarness(decls, probes)
		if err != nil {
			return rep, err
		}
		if len(got) != len(probes) {
			return rep, fmt.Errorf("certify: harness produced %d lines for %d probes (a function under validation may print)", len(got), len(probes))
		}
		rep.OK = applyCertResults(verdicts, vIndex, probes, got)
	}

	for _, v := range verdicts {
		if v.Verdict == "unknown" {
			rep.Skipped++
		} else {
			rep.Checked++
		}
	}
	sort.SliceStable(verdicts, func(i, j int) bool { return verdicts[i].Fn < verdicts[j].Fn })
	rep.Verdicts = verdicts
	rep.Bounds = falsifyBounds{SliceLenMax: falsSliceLenMax, IntDomain: falsIntDomain, CallDepth: falsCallDepth}
	return rep, nil
}

// applyCertResults diffs the compiled harness output against the interpreter's
// expected values and marks any diverging function "miscompiled". Returns whether
// the program is clean (no divergence). Extracted so the detection can be unit-tested.
func applyCertResults(verdicts []certVerdict, vIndex map[string]int, probes []certProbe, got []string) bool {
	ok := true
	for i, p := range probes {
		if i < len(got) && got[i] != p.source {
			vi := vIndex[p.fn]
			verdicts[vi].Verdict = "miscompiled"
			verdicts[vi].Expr = p.call
			verdicts[vi].Source = p.source
			verdicts[vi].Compiled = got[i]
			ok = false
		}
	}
	return ok
}

// certRank orders verdicts so the worst (most informative) wins when a function has
// multiple monomorphized instances.
func certRank(v string) int {
	switch v {
	case "miscompiled":
		return 5
	case "unknown":
		return 4
	case "partial":
		return 3
	case "certified-bounded":
		return 2
	case "certified":
		return 1
	}
	return 0
}

// bindArgList renders a tuple as a comma-separated MFL argument list.
func bindArgList(args []fval) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

// runCertHarness builds a single MFL program — the original decls plus a main that
// prints str(<call>) for each probe — compiles it, runs it, and returns the output
// lines. This is the CODEGEN semantics being validated.
func runCertHarness(decls []string, probes []certProbe) ([]string, error) {
	var b strings.Builder
	for _, d := range decls {
		if declName(d) == "main" {
			continue
		}
		b.WriteString(d)
		b.WriteString("\n")
	}
	// json() gives one canonical serialization for every type (scalars, strings, slices,
	// structs), matched exactly by renderCanonical on the interpreter side.
	b.WriteString("func main() {\n")
	for _, p := range probes {
		fmt.Fprintf(&b, "\tprintln(json(%s))\n", p.call)
	}
	b.WriteString("}\n")

	// split the harness blob into function decls the same way the CLI does.
	blocks, _, serr := splitFunctionsLoc(b.String())
	if serr != nil {
		return nil, fmt.Errorf("certify: harness split: %w", serr)
	}
	nd := make([]string, len(blocks))
	for i, bl := range blocks {
		nd[i] = normalize(bl)
	}
	prog, err := ParseProgram(nd)
	if err != nil {
		return nil, fmt.Errorf("certify: harness parse: %w", err)
	}
	bin, err := os.CreateTemp("", "mfl-certify-*")
	if err != nil {
		return nil, err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		return nil, fmt.Errorf("certify: harness build: %w", err)
	}
	out, err := exec.Command(bin.Name()).Output()
	if err != nil {
		return nil, fmt.Errorf("certify: harness run: %w", err)
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}
