package main

import (
	"fmt"
	"strconv"
)

type Parser struct {
	toks      []Token
	pos       int
	loopDepth int // >0 while parsing inside a while/for body; gates break/continue
}

func (p *Parser) peek() Token { return p.toks[p.pos] }
func (p *Parser) next() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) expect(kind TokKind, val string) (Token, error) {
	t := p.peek()
	if t.Kind != kind || (val != "" && t.Val != val) {
		return t, fmt.Errorf("expected %q, got %q at pos %d", val, t.Val, t.Pos)
	}
	return p.next(), nil
}

// ParseFunc parses a single decoded function source into a FuncDecl.
func ParseFunc(src string) (*FuncDecl, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	p := &Parser{toks: toks}
	fn, err := p.parseFuncDecl()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != TEOF {
		return nil, fmt.Errorf("trailing tokens after function: %q", p.peek().Val)
	}
	return fn, nil
}

func (p *Parser) parseFuncDecl() (*FuncDecl, error) {
	if _, err := p.expect(TKeyword, "func"); err != nil {
		return nil, err
	}
	nameTok, err := p.expect(TIdent, "")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TPunct, "("); err != nil {
		return nil, err
	}
	var params []string
	for p.peek().Val != ")" {
		pt, err := p.expect(TIdent, "")
		if err != nil {
			return nil, err
		}
		params = append(params, pt.Val)
		if p.peek().Val == "," {
			p.next()
		} else {
			break
		}
	}
	if _, err := p.expect(TPunct, ")"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FuncDecl{Name: nameTok.Val, Params: params, Body: body}, nil
}

func (p *Parser) parseBlock() ([]Stmt, error) {
	if _, err := p.expect(TPunct, "{"); err != nil {
		return nil, err
	}
	var stmts []Stmt
	for p.peek().Val != "}" && p.peek().Kind != TEOF {
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, s)
		// optional semicolons
		for p.peek().Val == ";" {
			p.next()
		}
	}
	if _, err := p.expect(TPunct, "}"); err != nil {
		return nil, err
	}
	return stmts, nil
}

func (p *Parser) parseStmt() (Stmt, error) {
	t := p.peek()
	if t.Kind == TKeyword {
		switch t.Val {
		case "return":
			p.next()
			if p.peek().Val == "}" || p.peek().Val == ";" {
				return &ReturnStmt{}, nil
			}
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			return &ReturnStmt{Val: x}, nil
		case "if":
			return p.parseIf()
		case "while":
			return p.parseWhile()
		case "for":
			return p.parseFor()
		case "break":
			p.next()
			if p.loopDepth == 0 {
				return nil, fmt.Errorf("break outside loop")
			}
			return &BreakStmt{}, nil
		case "continue":
			p.next()
			if p.loopDepth == 0 {
				return nil, fmt.Errorf("continue outside loop")
			}
			return &ContinueStmt{}, nil
		case "go":
			p.next()
			call, err := p.parsePostfix()
			if err != nil {
				return nil, err
			}
			c, ok := call.(*Call)
			if !ok {
				return nil, fmt.Errorf("go requires a function call")
			}
			return &GoStmt{Call: c}, nil
		case "var":
			p.next()
			nameTok, err := p.expect(TIdent, "")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TOp, "="); err != nil {
				return nil, err
			}
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			return &AssignStmt{Name: nameTok.Val, Op: ":=", Val: val}, nil
		}
	}
	// declaration: ident := expr
	if t.Kind == TIdent && p.toks[p.pos+1].Val == ":=" {
		name := p.next().Val
		p.next() // :=
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &AssignStmt{Name: name, Op: ":=", Val: val}, nil
	}
	// expression, possibly an assignment target (ident = / slice[i] =)
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().Val == "=" {
		p.next()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		switch lhs := x.(type) {
		case *Ident:
			return &AssignStmt{Name: lhs.Name, Op: "=", Val: val}, nil
		case *Index:
			return &IndexAssign{Target: lhs, Val: val}, nil
		default:
			return nil, fmt.Errorf("cannot assign to %T", x)
		}
	}
	return &ExprStmt{X: x}, nil
}

func (p *Parser) parseIf() (Stmt, error) {
	p.next() // if
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	then, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	var els []Stmt
	if p.peek().Val == "else" {
		p.next()
		if p.peek().Val == "if" {
			s, err := p.parseIf()
			if err != nil {
				return nil, err
			}
			els = []Stmt{s}
		} else {
			els, err = p.parseBlock()
			if err != nil {
				return nil, err
			}
		}
	}
	return &IfStmt{Cond: cond, Then: then, Else: els}, nil
}

func (p *Parser) parseWhile() (Stmt, error) {
	p.next() // while
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseLoopBody()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Cond: cond, Body: body}, nil
}

// parseFor handles Go's looping forms, desugared onto WhileStmt:
//   for { ... }        infinite loop
//   for cond { ... }   loop while cond
func (p *Parser) parseFor() (Stmt, error) {
	p.next() // for
	if p.peek().Val == "{" {
		body, err := p.parseLoopBody()
		if err != nil {
			return nil, err
		}
		return &WhileStmt{Cond: &BoolLit{Val: true}, Body: body}, nil
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseLoopBody()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Cond: cond, Body: body}, nil
}

// parseLoopBody parses a brace block while marking that we are inside a loop,
// so break/continue are accepted (and rejected outside any loop).
func (p *Parser) parseLoopBody() ([]Stmt, error) {
	p.loopDepth++
	body, err := p.parseBlock()
	p.loopDepth--
	return body, err
}

// ---- Expression parsing (precedence climbing) ----

var precedence = map[string]int{
	"||": 1,
	"&&": 2,
	"==": 3, "!=": 3,
	"<": 4, "<=": 4, ">": 4, ">=": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
}

func (p *Parser) parseExpr() (Expr, error) {
	return p.parseBinary(1)
}

func (p *Parser) parseBinary(minPrec int) (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TOp {
			break
		}
		prec, ok := precedence[t.Val]
		if !ok || prec < minPrec {
			break
		}
		op := p.next().Val
		right, err := p.parseBinary(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, L: left, R: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Expr, error) {
	t := p.peek()
	if t.Kind == TOp && (t.Val == "-" || t.Val == "!") {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: t.Val, X: x}, nil
	}
	return p.parsePostfix()
}

// parsePostfix parses a primary followed by any number of [index] operators.
func (p *Parser) parsePostfix() (Expr, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.peek().Val == "[" {
		p.next()
		idx, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TPunct, "]"); err != nil {
			return nil, err
		}
		x = &Index{X: x, Idx: idx}
	}
	return x, nil
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch t.Kind {
	case TInt:
		p.next()
		n, err := strconv.ParseInt(t.Val, 10, 64)
		if err != nil {
			return nil, err
		}
		return &IntLit{Val: n}, nil
	case TFloat:
		p.next()
		f, err := strconv.ParseFloat(t.Val, 64)
		if err != nil {
			return nil, err
		}
		return &FloatLit{Val: f}, nil
	case TString:
		p.next()
		return &StringLit{Val: t.Val}, nil
	case TKeyword:
		switch t.Val {
		case "true":
			p.next()
			return &BoolLit{Val: true}, nil
		case "false":
			p.next()
			return &BoolLit{Val: false}, nil
		case "nil":
			p.next()
			return &NilLit{}, nil
		}
		return nil, fmt.Errorf("unexpected keyword %q", t.Val)
	case TIdent:
		p.next()
		if p.peek().Val == "(" {
			return p.parseCall(t.Val)
		}
		return &Ident{Name: t.Val}, nil
	case TPunct:
		if t.Val == "(" {
			p.next()
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TPunct, ")"); err != nil {
				return nil, err
			}
			return x, nil
		}
		if t.Val == "[" {
			return p.parseSliceLit()
		}
	}
	return nil, fmt.Errorf("unexpected token %q at pos %d", t.Val, t.Pos)
}

// parseSliceLit parses a typed slice literal: []int{1, 2, 3} or []string{}.
func (p *Parser) parseSliceLit() (Expr, error) {
	if _, err := p.expect(TPunct, "["); err != nil {
		return nil, err
	}
	if _, err := p.expect(TPunct, "]"); err != nil {
		return nil, err
	}
	elemTok := p.next() // element type name
	if elemTok.Kind != TIdent {
		return nil, fmt.Errorf("slice element type must be a name, got %q", elemTok.Val)
	}
	if _, err := p.expect(TPunct, "{"); err != nil {
		return nil, err
	}
	var elems []Expr
	for p.peek().Val != "}" {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		elems = append(elems, e)
		if p.peek().Val == "," {
			p.next()
		} else {
			break
		}
	}
	if _, err := p.expect(TPunct, "}"); err != nil {
		return nil, err
	}
	return &SliceLit{Elem: elemTok.Val, Elems: elems}, nil
}

func (p *Parser) parseCall(callee string) (Expr, error) {
	p.next() // (
	var args []Expr
	for p.peek().Val != ")" {
		a, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if p.peek().Val == "," {
			p.next()
		} else {
			break
		}
	}
	if _, err := p.expect(TPunct, ")"); err != nil {
		return nil, err
	}
	return &Call{Callee: callee, Args: args}, nil
}
