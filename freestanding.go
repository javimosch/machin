package main

// The wasm-freestanding target: MFL for code with no operating system under it
// (a kernel, a bootloader, a host that supplies nothing). The output is a
// relocatable wasm object holding ONLY the program's functions plus the few
// inline helpers they can use: no libc, no WASI, no MFL runtime. Anything that
// would need the runtime is rejected here, by name, rather than left to surface
// as an undefined symbol at the embedder's link.
//
// The check is an allow-list: a language feature added later is rejected until
// someone decides it works without a runtime.

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// freestandingPrelude is the whole C environment of a freestanding program. The
// memory helpers are lifted from cRuntime so the two cannot drift apart.
func freestandingPrelude() string {
	var b strings.Builder
	b.WriteString("#include <stdint.h> /* a compiler header: needs no libc */\n")
	for _, line := range strings.Split(cRuntime, "\n") {
		for _, p := range []string{" mfl_peek_", " mfl_poke_", " mfl_f64_bits(", " mfl_f64_from_bits(", " mfl_ushr(", " mfl_shl(", " mfl_shr("} {
			if strings.HasPrefix(line, "static ") && strings.Contains(line, p) {
				if !strings.HasPrefix(line, "static inline ") {
					line = strings.Replace(line, "static ", "static inline ", 1)
				}
				b.WriteString(strings.ReplaceAll(line, "memcpy(", "__builtin_memcpy(") + "\n")
				break
			}
		}
	}
	b.WriteByte('\n')
	return b.String()
}

func freestandingBuiltin(name string) bool {
	switch name {
	case "int", "float", "f64_bits", "f64_from_bits", "ushr":
		return true
	}
	// Exactly the memory builtins the prelude defines.
	return (strings.HasPrefix(name, "peek_") || strings.HasPrefix(name, "poke_")) &&
		strings.Contains(freestandingPrelude(), " mfl_"+name+"(")
}

func freestandingCType(t string) bool { return t == "int64_t" || t == "double" || t == "int" }

type freestandingCheck struct {
	c       *Checker
	fn      string          // current instance
	name    string          // its source name, for messages
	userFns map[string]bool // functions declared in this program
	externs map[string]bool
}

func (f *freestandingCheck) errf(format string, a ...any) error {
	return fmt.Errorf("wasm-freestanding: in %s: %s", f.name, fmt.Sprintf(format, a...))
}

const freestandingWhy = "needs the MFL runtime, which this target does not have"

// checkFreestanding reports the first construct the target cannot compile.
func checkFreestanding(p *Program, c *Checker) error {
	f := &freestandingCheck{c: c, userFns: map[string]bool{}, externs: map[string]bool{}}
	if len(p.Types) > 0 {
		return fmt.Errorf("wasm-freestanding: type %s: structs are not supported yet", p.Types[0].Name)
	}
	for _, ed := range p.Externs {
		if len(ed.Links) > 0 {
			return fmt.Errorf("wasm-freestanding: extern %q: `link` does not apply, the embedder does the linking", ed.Lib)
		}
		for _, ef := range ed.Funcs {
			f.externs[ef.Name] = true
			for _, t := range append([]string{ef.Ret}, ef.Params...) {
				if strings.Contains(t, "string") || t == "bytes" || strings.HasPrefix(t, "func") || strings.HasSuffix(t, "*") {
					return fmt.Errorf("wasm-freestanding: extern %s: type %q is not supported (pass a pointer as ptr, and a length)", ef.Name, t)
				}
			}
		}
	}
	for _, fn := range p.Funcs {
		if fn.IsLambda {
			return fmt.Errorf("wasm-freestanding: closures and function values %s", freestandingWhy)
		}
		f.userFns[fn.Name] = true
	}
	// Globals are C static initializers: there is no startup code to run one.
	// (A constructor would be wrong too: wasm-ld may re-run constructors on every
	// exported call, silently resetting state.)
	for _, gv := range p.Globals {
		e := gv.Init
		if u, ok := e.(*Unary); ok && u.Op == "-" {
			e = u.X
		}
		switch e.(type) {
		case *IntLit, *FloatLit, *BoolLit:
		default:
			return fmt.Errorf("wasm-freestanding: global %s: the initial value must be an int, float or bool literal", gv.Name)
		}
		if !freestandingCType(c.GlobalCType(gv.Name)) {
			return fmt.Errorf("wasm-freestanding: global %s: only int, float and bool globals are supported", gv.Name)
		}
	}
	if len(c.ExportNames()) == 0 {
		return fmt.Errorf("wasm-freestanding: no exported functions — mark at least one with `export func`")
	}
	exported := map[string]int{}
	for _, inst := range c.Reps() {
		src := c.SrcFunc(inst)
		f.fn, f.name = inst, src.Name
		if src.Name == "main" {
			return f.errf("there is no program entry point here: use `export func` with another name")
		}
		for i := range src.Params {
			if !freestandingCType(c.ParamCType(inst, i)) {
				return f.errf("parameter %s: only int, float and bool are supported", src.Params[i])
			}
		}
		for i := 0; i < c.RetArity(inst); i++ {
			if !freestandingCType(c.RetCTypeAt(inst, i)) {
				return f.errf("only int, float and bool results are supported")
			}
		}
		for _, v := range c.Locals(inst) {
			if !freestandingCType(c.VarCType(inst, v)) {
				return f.errf("variable %s: only int, float and bool are supported (strings, slices, maps and channels %s)", v, freestandingWhy)
			}
		}
		if c.exportSrc[src.Name] {
			exported[src.Name]++
			if exported[src.Name] > 1 {
				return f.errf("an exported function cannot be used with more than one set of types")
			}
			if c.RetArity(inst) > 1 {
				return f.errf("an exported function can return at most one value")
			}
		}
		if err := f.stmts(src.Body); err != nil {
			return err
		}
	}
	return nil
}

func (f *freestandingCheck) stmts(body []Stmt) error {
	for _, s := range body {
		var err error
		switch n := s.(type) {
		case *ExprStmt:
			err = f.exprs(n.X)
		case *AssignStmt:
			err = f.exprs(n.Val)
		case *MultiAssign:
			err = f.exprs(n.Rhs...)
		case *ReturnStmt:
			err = f.exprs(n.Vals...)
		case *BreakStmt, *ContinueStmt:
		case *IfStmt:
			if err = f.exprs(n.Cond); err == nil {
				if err = f.stmts(n.Then); err == nil {
					err = f.stmts(n.Else)
				}
			}
		case *WhileStmt:
			if n.Cond != nil {
				err = f.exprs(n.Cond)
			}
			if err == nil {
				err = f.stmts(n.Body)
			}
		default:
			kind := strings.TrimSuffix(strings.TrimPrefix(fmt.Sprintf("%T", s), "*main."), "Stmt")
			err = f.errf("the %s statement is not supported", strings.ToLower(kind))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *freestandingCheck) exprs(es ...Expr) error {
	for _, e := range es {
		var err error
		switch n := e.(type) {
		case *IntLit, *FloatLit, *BoolLit, *Ident:
		case *StringLit:
			err = f.errf("string literals %s", freestandingWhy)
		case *Unary:
			err = f.exprs(n.X)
		case *Binary:
			err = f.exprs(n.L, n.R)
		case *Call:
			switch {
			case n.Spread:
				err = f.errf("%s(xs...): slices %s", n.Callee, freestandingWhy)
			case f.externs[n.Callee], f.userFns[n.Callee], freestandingBuiltin(n.Callee):
				err = f.exprs(n.Args...)
			default:
				err = f.errf("the builtin %s is not available: it %s", n.Callee, freestandingWhy)
			}
		default:
			err = f.errf("%s: this expression is not supported", exprStr(e))
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// freestandingExports renders a C-ABI function under each export's source name,
// so C code linked with the object can call it (the MFL symbol is mangled).
func (g *cgen) freestandingExports() string {
	insts := []string{}
	for _, inst := range g.c.Reps() {
		if g.c.exportSrc[g.c.SrcFunc(inst).Name] {
			insts = append(insts, inst)
		}
	}
	sort.Strings(insts)
	var b strings.Builder
	for _, inst := range insts {
		src := g.c.SrcFunc(inst)
		var params, args []string
		for i := range src.Params {
			params = append(params, fmt.Sprintf("%s a%d", g.c.ParamCType(inst, i), i))
			args = append(args, fmt.Sprintf("a%d", i))
		}
		ps := strings.Join(params, ", ")
		if ps == "" {
			ps = "void"
		}
		ret := ""
		if g.c.RetArity(inst) == 1 {
			ret = "return "
		}
		fmt.Fprintf(&b, "__attribute__((export_name(%q))) %s %s(%s) { %s%s(%s); }\n",
			src.Name, g.retType(inst), src.Name, ps, ret, g.c.CName(inst), strings.Join(args, ", "))
	}
	return b.String()
}

// freestandingArgs are the cc arguments for the object; cflags come last so an
// embedder can add -I, or wasm features such as -matomics.
func freestandingArgs(src, out string, cflags []string) []string {
	args := []string{"cc", "-target", "wasm32-freestanding", "-ffreestanding", "-nostdlib", "-fno-builtin",
		"-O2", "-fno-strict-aliasing", "-fwrapv", "-std=c11", "-c", "-o", out, src}
	return append(args, cflags...)
}

// BuildWasmFreestanding compiles the program to a relocatable wasm object.
func BuildWasmFreestanding(prog *Program, outPath string, cflags []string) error {
	csrc, _, err := CompileToCTarget(prog, false, targetWasmFreestanding)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "mfl-*.c")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(csrc); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	out, err := exec.Command(zigPath(), freestandingArgs(tmp.Name(), outPath, cflags)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %v\n%s", zigPath(), err, out)
	}
	return nil
}
