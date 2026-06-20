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
	c     *Checker
	buf   strings.Builder // function bodies
	tramp strings.Builder // goroutine trampolines
	goID  int
}

const cRuntime = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdarg.h>
#include <unistd.h>
#include <time.h>
#include <pthread.h>
#include <sys/socket.h>
#include <netinet/in.h>

/* slices: a Go-style header over an unboxed backing array */
typedef struct { void* data; int64_t len; int64_t cap; } mfl_slice;

static mfl_slice mfl_append(mfl_slice s, const void* elem, int64_t es) {
    if (s.len >= s.cap) {
        int64_t nc = s.cap ? s.cap * 2 : 4;
        s.data = realloc(s.data, nc * es); s.cap = nc;
    }
    memcpy((char*)s.data + s.len * es, elem, es);
    s.len++;
    return s;
}
static mfl_slice mfl_lit_i64(int64_t n, ...) {
    mfl_slice s = { n ? malloc(n * 8) : NULL, n, n };
    va_list ap; va_start(ap, n);
    for (int64_t i = 0; i < n; i++) ((int64_t*)s.data)[i] = va_arg(ap, int64_t);
    va_end(ap); return s;
}
static mfl_slice mfl_lit_f64(int64_t n, ...) {
    mfl_slice s = { n ? malloc(n * 8) : NULL, n, n };
    va_list ap; va_start(ap, n);
    for (int64_t i = 0; i < n; i++) ((double*)s.data)[i] = va_arg(ap, double);
    va_end(ap); return s;
}
static mfl_slice mfl_lit_str(int64_t n, ...) {
    mfl_slice s = { n ? malloc(n * sizeof(char*)) : NULL, n, n };
    va_list ap; va_start(ap, n);
    for (int64_t i = 0; i < n; i++) ((char**)s.data)[i] = va_arg(ap, char*);
    va_end(ap); return s;
}
static void mfl_sleep(int64_t ms) {
    struct timespec ts = { ms / 1000, (ms % 1000) * 1000000L };
    nanosleep(&ts, NULL);
}

static char* mfl_cat(const char* a, const char* b) {
    size_t la = strlen(a), lb = strlen(b);
    char* r = malloc(la + lb + 1);
    memcpy(r, a, la); memcpy(r + la, b, lb); r[la + lb] = 0;
    return r;
}
static char* mfl_str_i(int64_t v) { char* b = malloc(24); snprintf(b, 24, "%lld", (long long)v); return b; }
static char* mfl_str_d(double v)  { char* b = malloc(32); snprintf(b, 32, "%g", v); return b; }

/* networking: the low-level shape of Go's net package */
static int64_t mfl_listen(int64_t port) {
    int fd = socket(AF_INET, SOCK_STREAM, 0);
    int opt = 1; setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));
    struct sockaddr_in a; memset(&a, 0, sizeof(a));
    a.sin_family = AF_INET; a.sin_addr.s_addr = INADDR_ANY; a.sin_port = htons((uint16_t)port);
    if (bind(fd, (struct sockaddr*)&a, sizeof(a)) < 0) { perror("bind"); exit(1); }
    if (listen(fd, 64) < 0) { perror("listen"); exit(1); }
    return fd;
}
static int64_t mfl_accept(int64_t fd) { return accept((int)fd, NULL, NULL); }
static char* mfl_read(int64_t fd) {
    char* buf = malloc(65536);
    ssize_t n = read((int)fd, buf, 65535);
    if (n < 0) n = 0;
    buf[n] = 0;
    return buf;
}
static int64_t mfl_write(int64_t fd, const char* s) { return (int64_t)write((int)fd, s, strlen(s)); }
static void mfl_close(int64_t fd) { close((int)fd); }
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
	case KSlice:
		return "mfl_slice"
	}
	return "int64_t"
}

func cZero(k Kind) string {
	switch k {
	case KFloat:
		return "0.0"
	case KString:
		return "NULL"
	case KSlice:
		return "(mfl_slice){0}"
	default:
		return "0"
	}
}

func (g *cgen) program(funcs []*FuncDecl) (string, error) {
	// emit function bodies first (this also fills g.tramp via any go statements)
	for _, fn := range funcs {
		if err := g.function(fn); err != nil {
			return "", err
		}
	}
	var out strings.Builder
	out.WriteString(cRuntime)
	out.WriteByte('\n')
	for _, fn := range funcs {
		out.WriteString(g.signature(fn) + ";\n")
	}
	out.WriteByte('\n')
	out.WriteString(g.tramp.String())
	out.WriteString(g.buf.String())
	out.WriteString("int main(void) { mfl_main(); return 0; }\n")
	return out.String(), nil
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
	case *IndexAssign:
		x, err := g.expr(st.Target.X)
		if err != nil {
			return err
		}
		idx, err := g.expr(st.Target.Idx)
		if err != nil {
			return err
		}
		val, err := g.expr(st.Val)
		if err != nil {
			return err
		}
		ct := cType(g.c.ElemKindOf(st.Target.X))
		fmt.Fprintf(&g.buf, "((%s*)(%s).data)[%s] = %s;\n", ct, x, idx, val)
	case *GoStmt:
		return g.goStmt(st)
	default:
		return fmt.Errorf("codegen: unknown statement %T", s)
	}
	return nil
}

// goStmt spawns a pthread. For each go-call site it emits a per-site arg struct
// and trampoline, then a detached pthread_create at the call site.
func (g *cgen) goStmt(st *GoStmt) error {
	id := g.goID
	g.goID++
	callee := st.Call.Callee
	n := len(st.Call.Args)

	// arg struct + trampoline (a leading dummy field avoids an empty struct)
	fmt.Fprintf(&g.tramp, "struct mfl_go_%d { char _;", id)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&g.tramp, " %s a%d;", cType(g.c.ParamKind(callee, i)), i)
	}
	g.tramp.WriteString(" };\n")
	fmt.Fprintf(&g.tramp, "static void* mfl_go_run_%d(void* p) { struct mfl_go_%d* s = (struct mfl_go_%d*)p; mfl_%s(",
		id, id, id, callee)
	for i := 0; i < n; i++ {
		if i > 0 {
			g.tramp.WriteString(", ")
		}
		fmt.Fprintf(&g.tramp, "s->a%d", i)
	}
	g.tramp.WriteString("); free(s); return NULL; }\n")

	// call site
	g.buf.WriteString("{\n")
	fmt.Fprintf(&g.buf, "        struct mfl_go_%d* s = malloc(sizeof(*s));\n", id)
	for i, a := range st.Call.Args {
		e, err := g.expr(a)
		if err != nil {
			return err
		}
		fmt.Fprintf(&g.buf, "        s->a%d = (%s);\n", i, e)
	}
	fmt.Fprintf(&g.buf, "        pthread_t t; pthread_create(&t, NULL, mfl_go_run_%d, s); pthread_detach(t);\n", id)
	g.buf.WriteString("    }\n")
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
	case *SliceLit:
		return g.sliceLit(ex)
	case *Index:
		x, err := g.expr(ex.X)
		if err != nil {
			return "", err
		}
		idx, err := g.expr(ex.Idx)
		if err != nil {
			return "", err
		}
		ct := cType(g.c.ElemKindOf(ex.X))
		return fmt.Sprintf("((%s*)(%s).data)[%s]", ct, x, idx), nil
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
	// String (in)equality must compare contents, not pointers.
	if (ex.Op == "==" || ex.Op == "!=") && g.c.NodeKind(ex.L) == KString {
		if ex.Op == "==" {
			return fmt.Sprintf("(strcmp(%s, %s) == 0)", l, r), nil
		}
		return fmt.Sprintf("(strcmp(%s, %s) != 0)", l, r), nil
	}
	return fmt.Sprintf("(%s %s %s)", l, ex.Op, r), nil
}

func (g *cgen) sliceLit(ex *SliceLit) (string, error) {
	ek := g.c.ElemKindOf(ex)
	builder, cast := "mfl_lit_i64", "int64_t"
	switch ek {
	case KFloat:
		builder, cast = "mfl_lit_f64", "double"
	case KString:
		builder, cast = "mfl_lit_str", "char*"
	}
	parts := []string{strconv.Itoa(len(ex.Elems))}
	for _, el := range ex.Elems {
		e, err := g.expr(el)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("(%s)(%s)", cast, e))
	}
	return builder + "(" + strings.Join(parts, ", ") + ")", nil
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
		if g.c.NodeKind(ex.Args[0]) == KSlice {
			return fmt.Sprintf("((%s).len)", args[0]), nil
		}
		return fmt.Sprintf("((int64_t)strlen(%s))", args[0]), nil
	case "append":
		ct := cType(g.c.ElemKindOf(ex.Args[0]))
		return fmt.Sprintf("mfl_append(%s, &(%s){%s}, sizeof(%s))", args[0], ct, args[1], ct), nil
	case "sleep":
		return fmt.Sprintf("mfl_sleep(%s)", args[0]), nil
	case "str":
		if g.c.NodeKind(ex.Args[0]) == KFloat {
			return fmt.Sprintf("mfl_str_d(%s)", args[0]), nil
		}
		return fmt.Sprintf("mfl_str_i(%s)", args[0]), nil
	case "int":
		return fmt.Sprintf("((int64_t)(%s))", args[0]), nil
	case "listen":
		return fmt.Sprintf("mfl_listen(%s)", args[0]), nil
	case "accept":
		return fmt.Sprintf("mfl_accept(%s)", args[0]), nil
	case "read":
		return fmt.Sprintf("mfl_read(%s)", args[0]), nil
	case "write":
		return fmt.Sprintf("mfl_write(%s, %s)", args[0], args[1]), nil
	case "close":
		return fmt.Sprintf("mfl_close(%s)", args[0]), nil
	case "print", "println":
		return "", fmt.Errorf("print/println may only be used as a statement")
	}
	return fmt.Sprintf("mfl_%s(%s)", ex.Callee, strings.Join(args, ", ")), nil
}
