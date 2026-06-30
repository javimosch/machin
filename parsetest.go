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
	if len(args) >= 2 && args[0] == "--program" {
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		structs := collectStructNames(lines)
		var out strings.Builder
		for _, ln := range lines { // classify + parse + dump each decl in source order
			toks, e := Lex(ln)
			if e != nil || len(toks) == 0 || toks[0].Kind != TKeyword {
				continue
			}
			switch toks[0].Val {
			case "type":
				if td, err := ParseType(ln); err != nil {
					out.WriteString("(parse-error)\n")
				} else {
					out.WriteString(sexprType(td) + "\n")
				}
			case "extern":
				if ed, err := ParseExtern(ln); err != nil {
					out.WriteString("(parse-error)\n")
				} else {
					out.WriteString(sexprExtern(ed) + "\n")
				}
			case "var":
				if gv, err := ParseGlobalWith(ln, structs); err != nil {
					out.WriteString("(parse-error)\n")
				} else {
					out.WriteString(sexprGlobal(gv) + "\n")
				}
			case "func", "export":
				if fn, err := ParseFuncWith(ln, structs); err != nil {
					out.WriteString("(parse-error)\n")
				} else {
					out.WriteString(sexprFunc(fn) + "\n")
				}
			}
		}
		fmt.Print(out.String())
		return nil
	}
	if len(args) >= 2 && args[0] == "--funcs" {
		data, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		structs := collectStructNames(lines)
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
	return fmt.Errorf("usage: machin parsetest --expr <e> | --func <src> | --funcs <f.mfl> | --program <f.mfl>")
}

// collectStructNames scans every line for `type NAME` and `cstruct NAME`, so
// `T{...}` composite literals are recognized when parsing functions/globals.
func collectStructNames(lines []string) map[string]bool {
	structs := map[string]bool{}
	for _, ln := range lines {
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
	return structs
}

func sexprFields(names []string, types []string) string {
	s := "(fields"
	for i := range names {
		s += " (f " + names[i] + " " + types[i] + ")"
	}
	return s + ")"
}

func sexprType(td *TypeDecl) string {
	names := make([]string, len(td.Fields))
	types := make([]string, len(td.Fields))
	for i, f := range td.Fields {
		names[i], types[i] = f.Name, f.Type
	}
	return "(type " + td.Name + " " + sexprFields(names, types) + ")"
}

func sexprGlobal(gv *GlobalVar) string {
	return "(global " + gv.Name + " " + sexprExpr(gv.Init) + ")"
}

func sexprExtern(ed *ExternDecl) string {
	s := "(extern " + ed.Lib + " (header " + ed.Header + ") (links"
	for _, l := range ed.Links {
		s += " " + l
	}
	s += ") (cflags " + ed.CFlags + ") (cstructs"
	for _, cs := range ed.Structs {
		names := make([]string, len(cs.Fields))
		types := make([]string, len(cs.Fields))
		for i, f := range cs.Fields {
			names[i], types[i] = f.Name, f.CType
		}
		s += " (cstruct " + cs.Name + " " + sexprFields(names, types) + ")"
	}
	s += ") (fns"
	for _, fn := range ed.Funcs {
		s += " (fn " + fn.Name + " (params"
		for _, p := range fn.Params {
			s += " " + p
		}
		s += ") " + fn.Ret + ")"
	}
	return s + "))"
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
