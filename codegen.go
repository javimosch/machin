package main

import (
	"fmt"
	"strconv"
	"strings"
)

// CompileToC type-checks the program and emits standalone C99. The C is fed to
// `cc -O2`, so MFL's runtime cost is whatever the C compiler's optimizer
// produces — native, on par with C/Rust/Zig for scalar code.
func CompileToC(funcs []*FuncDecl) (string, error) {
	c, err := Check(funcs)
	if err != nil {
		return "", err
	}
	g := &cgen{c: c}
	return g.program(funcs)
}

type cgen struct {
	c   *Checker
	buf strings.Builder
}

const cRuntime = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

static char* mfl_cat(const char* a, const char* b) {
    size_t la = strlen(a), lb = strlen(b);
    char* r = malloc(la + lb + 1);
    memcpy(r, a, la); memcpy(r + la, b, lb); r[la + lb] = 0;
    return r;
}
static char* mfl_str_i(int64_t v) { char* b = malloc(24); snprintf(b, 24, "%lld", (long long)v); return b; }
static char* mfl_str_d(double v)  { char* b = malloc(32); snprintf(b, 32, "%g", v); return b; }
`

func cType(k Kind) string {
	switch k {
	case KInt:
		return "int64_t"
	case KFloat:
		return "double"
	case KBool:
		return "int"
	case KString:
		return "char*"
	case KVoid:
		return "void"
	}
	return "int64_t"
}

func cZero(k Kind) string {
	switch k {
	case KFloat:
		return "0.0"
	case KString:
		return "NULL"
	default:
		return "0"
	}
}

func (g *cgen) program(funcs []*FuncDecl) (string, error) {
	g.buf.WriteString(cRuntime)
	g.buf.WriteByte('\n')
	// forward declarations
	for _, fn := range funcs {
		g.buf.WriteString(g.signature(fn) + ";\n")
	}
	g.buf.WriteByte('\n')
	// definitions
	for _, fn := range funcs {
		if err := g.function(fn); err != nil {
			return "", err
		}
	}
	g.buf.WriteString("int main(void) { mfl_main(); return 0; }\n")
	return g.buf.String(), nil
}

func (g *cgen) signature(fn *FuncDecl) string {
	ret := cType(g.c.RetKind(fn.Name))
	parts := make([]string, len(fn.Params))
	for i, p := range fn.Params {
		parts[i] = cType(g.c.ParamKind(fn.Name, i)) + " v_" + p
	}
	params := strings.Join(parts, ", ")
	if params == "" {
		params = "void"
	}
	return fmt.Sprintf("%s mfl_%s(%s)", ret, fn.Name, params)
}

func (g *cgen) function(fn *FuncDecl) error {
	g.buf.WriteString(g.signature(fn) + " {\n")
	for _, name := range g.c.Locals(fn.Name) {
		k := g.c.VarKind(fn.Name, name)
		fmt.Fprintf(&g.buf, "    %s v_%s = %s;\n", cType(k), name, cZero(k))
	}
	for _, s := range fn.Body {
		if err := g.stmt(s, 1); err != nil {
			return err
		}
	}
	g.buf.WriteString("}\n\n")
	return nil
}

func indentC(b *strings.Builder, n int) {
	for i := 0; i < n; i++ {
		b.WriteString("    ")
	}
}

func (g *cgen) stmt(s Stmt, depth int) error {
	indentC(&g.buf, depth)
	switch st := s.(type) {
	case *ExprStmt:
		if call, ok := st.X.(*Call); ok && (call.Callee == "print" || call.Callee == "println") {
			return g.printCall(call, depth)
		}
		e, err := g.expr(st.X)
		if err != nil {
			return err
		}
		g.buf.WriteString(e + ";\n")
	case *AssignStmt:
		e, err := g.expr(st.Val)
		if err != nil {
			return err
		}
		fmt.Fprintf(&g.buf, "v_%s = %s;\n", st.Name, e)
	case *ReturnStmt:
		if st.Val == nil {
			g.buf.WriteString("return;\n")
			return nil
		}
		e, err := g.expr(st.Val)
		if err != nil {
			return err
		}
		g.buf.WriteString("return " + e + ";\n")
	case *IfStmt:
		cond, err := g.expr(st.Cond)
		if err != nil {
			return err
		}
		g.buf.WriteString("if (" + cond + ") {\n")
		for _, t := range st.Then {
			if err := g.stmt(t, depth+1); err != nil {
				return err
			}
		}
		indentC(&g.buf, depth)
		if st.Else != nil {
			g.buf.WriteString("} else {\n")
			for _, e := range st.Else {
				if err := g.stmt(e, depth+1); err != nil {
					return err
				}
			}
			indentC(&g.buf, depth)
		}
		g.buf.WriteString("}\n")
	case *WhileStmt:
		cond, err := g.expr(st.Cond)
		if err != nil {
			return err
		}
		g.buf.WriteString("while (" + cond + ") {\n")
		for _, t := range st.Body {
			if err := g.stmt(t, depth+1); err != nil {
				return err
			}
		}
		indentC(&g.buf, depth)
		g.buf.WriteString("}\n")
	default:
		return fmt.Errorf("codegen: unknown statement %T", s)
	}
	return nil
}

// printCall emits one print per argument, with single-space separators, so no
// runtime variadic machinery is needed.
func (g *cgen) printCall(call *Call, depth int) error {
	for i, a := range call.Args {
		if i > 0 {
			g.buf.WriteString("fputs(\" \", stdout); ")
		}
		e, err := g.expr(a)
		if err != nil {
			return err
		}
		switch g.c.NodeKind(a) {
		case KInt:
			fmt.Fprintf(&g.buf, "printf(\"%%lld\", (long long)(%s));", e)
		case KFloat:
			fmt.Fprintf(&g.buf, "printf(\"%%g\", (double)(%s));", e)
		case KBool:
			fmt.Fprintf(&g.buf, "fputs((%s) ? \"true\" : \"false\", stdout);", e)
		case KString:
			fmt.Fprintf(&g.buf, "fputs((%s), stdout);", e)
		default:
			fmt.Fprintf(&g.buf, "printf(\"%%lld\", (long long)(%s));", e)
		}
		g.buf.WriteByte('\n')
		if i < len(call.Args)-1 {
			indentC(&g.buf, depth)
		}
	}
	if len(call.Args) == 0 {
		// keep alignment for a bare println()
	}
	indentC(&g.buf, depth)
	if call.Callee == "println" {
		g.buf.WriteString("fputs(\"\\n\", stdout);\n")
	} else {
		g.buf.WriteString("fflush(stdout);\n")
	}
	return nil
}

func (g *cgen) expr(e Expr) (string, error) {
	switch ex := e.(type) {
	case *IntLit:
		return strconv.FormatInt(ex.Val, 10), nil
	case *FloatLit:
		s := strconv.FormatFloat(ex.Val, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s, nil
	case *StringLit:
		return strconv.Quote(ex.Val), nil
	case *BoolLit:
		if ex.Val {
			return "1", nil
		}
		return "0", nil
	case *NilLit:
		return "0", nil
	case *Ident:
		return "v_" + ex.Name, nil
	case *Unary:
		x, err := g.expr(ex.X)
		if err != nil {
			return "", err
		}
		return "(" + ex.Op + x + ")", nil
	case *Binary:
		return g.binary(ex)
	case *Call:
		return g.call(ex)
	}
	return "", fmt.Errorf("codegen: unknown expression %T", e)
}

func (g *cgen) binary(ex *Binary) (string, error) {
	l, err := g.expr(ex.L)
	if err != nil {
		return "", err
	}
	r, err := g.expr(ex.R)
	if err != nil {
		return "", err
	}
	if ex.Op == "+" && g.c.NodeKind(ex) == KString {
		return fmt.Sprintf("mfl_cat(%s, %s)", l, r), nil
	}
	return fmt.Sprintf("(%s %s %s)", l, ex.Op, r), nil
}

func (g *cgen) call(ex *Call) (string, error) {
	args := make([]string, len(ex.Args))
	for i, a := range ex.Args {
		s, err := g.expr(a)
		if err != nil {
			return "", err
		}
		args[i] = s
	}
	switch ex.Callee {
	case "len":
		return fmt.Sprintf("((int64_t)strlen(%s))", args[0]), nil
	case "str":
		if g.c.NodeKind(ex.Args[0]) == KFloat {
			return fmt.Sprintf("mfl_str_d(%s)", args[0]), nil
		}
		return fmt.Sprintf("mfl_str_i(%s)", args[0]), nil
	case "int":
		return fmt.Sprintf("((int64_t)(%s))", args[0]), nil
	case "print", "println":
		return "", fmt.Errorf("print/println may only be used as a statement")
	}
	return fmt.Sprintf("mfl_%s(%s)", ex.Callee, strings.Join(args, ", ")), nil
}
