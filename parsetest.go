package main

import (
	"fmt"
	"os"
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
	return fmt.Errorf("usage: machin parsetest --expr <expression>")
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
		return fmt.Sprintf("(float %v)", v.Val)
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
		return "(funclit)" // body dumped once the statement stage lands
	}
	fmt.Fprintf(os.Stderr, "sexpr: unhandled %T\n", e)
	return "(?)"
}
