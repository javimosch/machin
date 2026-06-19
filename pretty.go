package main

import (
	"fmt"
	"strconv"
	"strings"
)

// prettyFunc renders a FuncDecl back into readable, indented source.
func prettyFunc(fn *FuncDecl) string {
	var b strings.Builder
	fmt.Fprintf(&b, "func %s(%s) {\n", fn.Name, strings.Join(fn.Params, ", "))
	for _, s := range fn.Body {
		writeStmt(&b, s, 1)
	}
	b.WriteString("}")
	return b.String()
}

func indent(b *strings.Builder, n int) {
	for i := 0; i < n; i++ {
		b.WriteString("    ")
	}
}

func writeStmt(b *strings.Builder, s Stmt, depth int) {
	indent(b, depth)
	switch st := s.(type) {
	case *ExprStmt:
		b.WriteString(writeExpr(st.X))
		b.WriteByte('\n')
	case *AssignStmt:
		fmt.Fprintf(b, "%s %s %s\n", st.Name, st.Op, writeExpr(st.Val))
	case *ReturnStmt:
		if st.Val == nil {
			b.WriteString("return\n")
		} else {
			fmt.Fprintf(b, "return %s\n", writeExpr(st.Val))
		}
	case *IfStmt:
		fmt.Fprintf(b, "if %s {\n", writeExpr(st.Cond))
		for _, t := range st.Then {
			writeStmt(b, t, depth+1)
		}
		indent(b, depth)
		if st.Else != nil {
			b.WriteString("} else {\n")
			for _, e := range st.Else {
				writeStmt(b, e, depth+1)
			}
			indent(b, depth)
		}
		b.WriteString("}\n")
	case *WhileStmt:
		fmt.Fprintf(b, "while %s {\n", writeExpr(st.Cond))
		for _, t := range st.Body {
			writeStmt(b, t, depth+1)
		}
		indent(b, depth)
		b.WriteString("}\n")
	default:
		fmt.Fprintf(b, "<?stmt %T>\n", s)
	}
}

func writeExpr(e Expr) string {
	switch ex := e.(type) {
	case *IntLit:
		return strconv.FormatInt(ex.Val, 10)
	case *FloatLit:
		return strconv.FormatFloat(ex.Val, 'g', -1, 64)
	case *StringLit:
		return strconv.Quote(ex.Val)
	case *BoolLit:
		if ex.Val {
			return "true"
		}
		return "false"
	case *NilLit:
		return "nil"
	case *Ident:
		return ex.Name
	case *Unary:
		return ex.Op + writeExpr(ex.X)
	case *Binary:
		return fmt.Sprintf("(%s %s %s)", writeExpr(ex.L), ex.Op, writeExpr(ex.R))
	case *Call:
		args := make([]string, len(ex.Args))
		for i, a := range ex.Args {
			args[i] = writeExpr(a)
		}
		return fmt.Sprintf("%s(%s)", ex.Callee, strings.Join(args, ", "))
	}
	return fmt.Sprintf("<?expr %T>", e)
}
