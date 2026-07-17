package main

// render.go — a compact AST→source renderer for the optimizer's reports. It covers the
// arithmetic/control-flow subset that `machin optimize` rewrites; any node it can't render
// flips the `complete` flag so the caller withholds the "optimized source" (never prints a
// half-rendered function). Precedence-aware so the emitted source reparses to the same tree.

import (
	"strconv"
	"strings"
)

// optBinPrec ranks binary operators; higher binds tighter. Used to insert minimal parentheses.
var optBinPrec = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3,
	"|": 4, "^": 5, "&": 6,
	"<<": 7, ">>": 7,
	"+": 8, "-": 8,
	"*": 9, "/": 9, "%": 9,
}

type renderer struct{ complete bool }

func renderFuncSrc(f *FuncDecl) (string, bool) {
	r := &renderer{complete: true}
	var b strings.Builder
	b.WriteString("func ")
	b.WriteString(f.Name)
	b.WriteByte('(')
	b.WriteString(strings.Join(f.Params, ", "))
	b.WriteByte(')')
	if len(f.Returns) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(f.Returns, ", "))
		b.WriteByte(')')
	}
	b.WriteString(" { ")
	b.WriteString(r.stmts(f.Body))
	b.WriteString("}")
	return b.String(), r.complete
}

// renderStmtsSrc renders a statement list for equality comparison (no completeness needed).
func renderStmtsSrc(ss []Stmt) string {
	r := &renderer{complete: true}
	return r.stmts(ss)
}

func (r *renderer) stmts(ss []Stmt) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(r.stmt(s))
		b.WriteByte(' ')
	}
	return b.String()
}

func (r *renderer) stmt(s Stmt) string {
	switch x := s.(type) {
	case *ExprStmt:
		return r.expr(x.X, 0)
	case *AssignStmt:
		return x.Name + " " + x.Op + " " + r.expr(x.Val, 0)
	case *MultiAssign:
		vals := make([]string, len(x.Rhs))
		for i, e := range x.Rhs {
			vals[i] = r.expr(e, 0)
		}
		return strings.Join(x.Names, ", ") + " " + x.Op + " " + strings.Join(vals, ", ")
	case *ReturnStmt:
		if len(x.Vals) == 0 {
			return "return"
		}
		vals := make([]string, len(x.Vals))
		for i, e := range x.Vals {
			vals[i] = r.expr(e, 0)
		}
		return "return " + strings.Join(vals, ", ")
	case *BreakStmt:
		return "break"
	case *ContinueStmt:
		return "continue"
	case *IfStmt:
		out := "if " + r.expr(x.Cond, 0) + " { " + r.stmts(x.Then) + "}"
		if len(x.Else) > 0 {
			out += " else { " + r.stmts(x.Else) + "}"
		}
		return out
	case *WhileStmt:
		return "for " + r.expr(x.Cond, 0) + " { " + r.stmts(x.Body) + "}"
	case *RangeStmt:
		lhs := x.Key
		if x.Val != "" {
			lhs += ", " + x.Val
		}
		return "for " + lhs + " := range " + r.expr(x.X, 0) + " { " + r.stmts(x.Body) + "}"
	case *IndexAssign:
		return r.expr(x.Target.X, 10) + "[" + r.expr(x.Target.Idx, 0) + "] = " + r.expr(x.Val, 0)
	case *FieldAssign:
		return r.expr(x.Target.X, 10) + "." + x.Target.Name + " = " + r.expr(x.Val, 0)
	case *SendStmt:
		return r.expr(x.Ch, 10) + " <- " + r.expr(x.Val, 0)
	case *ArenaStmt:
		return "arena { " + r.stmts(x.Body) + "}"
	default:
		r.complete = false
		return "/*?*/"
	}
}

func (r *renderer) expr(e Expr, parentPrec int) string {
	switch x := e.(type) {
	case *IntLit:
		return strconv.FormatInt(x.Val, 10)
	case *FloatLit:
		return strconv.FormatFloat(x.Val, 'g', -1, 64)
	case *StringLit:
		return strconv.Quote(x.Val)
	case *BoolLit:
		if x.Val {
			return "true"
		}
		return "false"
	case *NilLit:
		return "nil"
	case *Ident:
		return x.Name
	case *Unary:
		return x.Op + r.expr(x.X, 10)
	case *Binary:
		p := optBinPrec[x.Op]
		s := r.expr(x.L, p) + " " + x.Op + " " + r.expr(x.R, p+1)
		if p < parentPrec {
			return "(" + s + ")"
		}
		return s
	case *Call:
		return x.Callee + "(" + r.args(x.Args, x.Spread) + ")"
	case *CallValue:
		return r.expr(x.Fn, 10) + "(" + r.args(x.Args, false) + ")"
	case *Index:
		return r.expr(x.X, 10) + "[" + r.expr(x.Idx, 0) + "]"
	case *FieldAccess:
		return r.expr(x.X, 10) + "." + x.Name
	case *SliceLit:
		return "[]" + x.Elem + "{" + r.args(x.Elems, false) + "}"
	case *StructLit:
		if len(x.FieldNames) == len(x.Vals) && len(x.FieldNames) > 0 {
			parts := make([]string, len(x.Vals))
			for i, v := range x.Vals {
				parts[i] = x.FieldNames[i] + ": " + r.expr(v, 0)
			}
			return x.Type + "{" + strings.Join(parts, ", ") + "}"
		}
		return x.Type + "{" + r.args(x.Vals, false) + "}"
	default:
		r.complete = false
		return "/*?*/"
	}
}

func (r *renderer) args(es []Expr, spread bool) string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = r.expr(e, 0)
	}
	out := strings.Join(parts, ", ")
	if spread {
		out += "..."
	}
	return out
}
