package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// cmdParseTest is the stage-2 oracle: parse with the Go parser and print the AST
// as canonical, fully-parenthesized S-expressions. The MFL parser (selfhost/
// parse.src) emits the identical form; the two are diffed.
//
//	machin parsetest --expr "<expression>"   dump one expression's AST
//
// (Program/statement modes are added as those stages of the MFL parser land.)
// String values are hex-encoded to sidestep every escaping mismatch.
func cmdParseTest(args []string) error {
	if len(args) >= 2 && args[0] == "--expr" {
		toks, err := Lex(args[1])
		if err != nil {
			return err
		}
		p := &Parser{toks: toks}
		e, err := p.parseExpr()
		if err != nil {
			return err
		}
		if p.peek().Kind != TEOF {
			return fmt.Errorf("trailing tokens after expression: %q", p.peek().Val)
		}
		fmt.Println(sexprExpr(e))
		return nil
	}
	if len(args) >= 2 && args[0] == "--func" {
		fn, err := ParseFunc(args[1])
		if err != nil {
			return err
		}
		fmt.Println(sexprFunc(fn))
		return nil
	}
	if len(args) >= 2 && args[0] == "--funcs" {
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		structs := map[string]bool{}
		for _, ln := range lines { // pass 1: collect struct names (type decls + cstruct/FFI handles)
			toks, e := Lex(ln)
			if e != nil {
				continue
			}
			for i := 0; i+1 < len(toks); i++ {
				if (toks[i].Val == "type" || toks[i].Val == "cstruct") && toks[i+1].Kind == TIdent {
					structs[toks[i+1].Val] = true
				}
			}
		}
		var out strings.Builder
		for _, ln := range lines { // pass 2: parse + dump every function
			if strings.HasPrefix(ln, "func ") || strings.HasPrefix(ln, "export func ") {
				fn, err := ParseFuncWith(ln, structs)
				if err != nil {
					out.WriteString("(parse-error)\n")
					continue
				}
				out.WriteString(sexprFunc(fn) + "\n")
			}
		}
		fmt.Print(out.String())
		return nil
	}
	return fmt.Errorf("usage: machin parsetest --expr <e> | --func <src> | --funcs <file.mfl>")
}

func sexprStmts(ss []Stmt) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteByte(' ')
		b.WriteString(sexprStmt(s))
	}
	return b.String()
}

func sexprStmt(s Stmt) string {
	switch v := s.(type) {
	case *ExprStmt:
		return "(expr " + sexprExpr(v.X) + ")"
	case *AssignStmt:
		return "(assign " + v.Op + " " + v.Name + " " + sexprExpr(v.Val) + ")"
	case *MultiAssign:
		names := "(names"
		for _, n := range v.Names {
			names += " " + n
		}
		names += ")"
		return "(multi " + v.Op + " " + names + sexprExprs(v.Rhs) + ")"
	case *ReturnStmt:
		return "(return" + sexprExprs(v.Vals) + ")"
	case *BreakStmt:
		return "(break)"
	case *ContinueStmt:
		return "(continue)"
	case *IfStmt:
		return "(if " + sexprExpr(v.Cond) + " (then" + sexprStmts(v.Then) + ") (else" + sexprStmts(v.Else) + "))"
	case *WhileStmt:
		return "(while " + sexprExpr(v.Cond) + " (body" + sexprStmts(v.Body) + "))"
	case *RangeStmt:
		return "(range " + v.Key + " " + v.Val + " " + sexprExpr(v.X) + " (body" + sexprStmts(v.Body) + "))"
	case *IndexAssign:
		return "(idxassign " + sexprExpr(v.Target) + " " + sexprExpr(v.Val) + ")"
	case *FieldAssign:
		return "(fldassign " + sexprExpr(v.Target) + " " + sexprExpr(v.Val) + ")"
	case *SendStmt:
		return "(send " + sexprExpr(v.Ch) + " " + sexprExpr(v.Val) + ")"
	case *GoStmt:
		return "(go " + sexprExpr(v.Call) + ")"
	case *ArenaStmt:
		return "(arena (body" + sexprStmts(v.Body) + "))"
	case *SelectStmt:
		s := "(select"
		for _, c := range v.Cases {
			if c.RecvCh != nil {
				s += " (case recv " + c.Name + " " + c.OkName + " " + sexprExpr(c.RecvCh) + " (body" + sexprStmts(c.Body) + "))"
			} else {
				s += " (case send " + sexprExpr(c.SendCh) + " " + sexprExpr(c.SendVal) + " (body" + sexprStmts(c.Body) + "))"
			}
		}
		if v.HasDefault {
			s += " (default (body" + sexprStmts(v.Default) + "))"
		}
		return s + ")"
	}
	fmt.Fprintf(os.Stderr, "sexpr: unhandled stmt %T\n", s)
	return "(?s)"
}

func sexprFunc(fn *FuncDecl) string {
	s := "(func " + fn.Name
	if fn.Exported {
		s += " export"
	}
	s += " (params"
	for i, p := range fn.Params {
		s += " " + p
		if fn.Variadic && i == len(fn.Params)-1 {
			s += "..."
		}
	}
	s += ") (returns"
	for _, r := range fn.Returns {
		s += " " + r
	}
	s += ") (body" + sexprStmts(fn.Body) + "))"
	return s
}

func sexprExprs(es []Expr) string {
	var b strings.Builder
	for _, e := range es {
		b.WriteByte(' ')
		b.WriteString(sexprExpr(e))
	}
	return b.String()
}

func sexprExpr(e Expr) string {
	switch v := e.(type) {
	case *IntLit:
		return fmt.Sprintf("(int %d)", v.Val)
	case *FloatLit:
		// shortest decimal WITHOUT exponent, so the MFL side (lexeme with trailing
		// zeros stripped) matches exactly — avoids %v's 1e-05 / 1e+21 forms.
		return "(float " + strconv.FormatFloat(v.Val, 'f', -1, 64) + ")"
	case *StringLit:
		return fmt.Sprintf("(str %x)", v.Val)
	case *BoolLit:
		if v.Val {
			return "(bool true)"
		}
		return "(bool false)"
	case *NilLit:
		return "(nil)"
	case *Ident:
		return "(id " + v.Name + ")"
	case *Unary:
		return "(unary " + v.Op + " " + sexprExpr(v.X) + ")"
	case *Binary:
		return "(bin " + v.Op + " " + sexprExpr(v.L) + " " + sexprExpr(v.R) + ")"
	case *Call:
		s := "(call " + v.Callee + sexprExprs(v.Args)
		if v.Spread {
			s += " ..."
		}
		return s + ")"
	case *CallValue:
		return "(callv " + sexprExpr(v.Fn) + sexprExprs(v.Args) + ")"
	case *Index:
		return "(index " + sexprExpr(v.X) + " " + sexprExpr(v.Idx) + ")"
	case *FieldAccess:
		return "(field " + sexprExpr(v.X) + " " + v.Name + ")"
	case *SliceLit:
		return "(slice " + v.Elem + sexprExprs(v.Elems) + ")"
	case *StructLit:
		keys := "(keys"
		for _, n := range v.FieldNames {
			keys += " " + n
		}
		keys += ")"
		return "(struct " + v.Type + " " + keys + sexprExprs(v.Vals) + ")"
	case *MakeChan:
		return "(makechan " + v.Elem + ")"
	case *MakeMap:
		return "(makemap " + v.Key + " " + v.Val + ")"
	case *Recv:
		return "(recv " + sexprExpr(v.Ch) + ")"
	case *FuncLit:
		params := "(params"
		for _, p := range v.Params {
			params += " " + p
		}
		params += ")"
		return "(funclit " + params + " (body" + sexprStmts(v.Body) + "))"
	}
	fmt.Fprintf(os.Stderr, "sexpr: unhandled %T\n", e)
	return "(?)"
}
