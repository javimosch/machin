package main

import (
	"fmt"
	"strconv"
)

type Parser struct {
	toks    []Token
	pos     int
	structs map[string]bool // known struct type names (for T{...} literals)
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
func ParseFunc(src string) (*FuncDecl, error) { return ParseFuncWith(src, nil) }

// ParseFuncWith parses a function, recognizing T{...} literals for the given
// known struct type names.
func ParseFuncWith(src string, structs map[string]bool) (*FuncDecl, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	p := &Parser{toks: toks, structs: structs}
	fn, err := p.parseFuncDecl()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != TEOF {
		return nil, fmt.Errorf("trailing tokens after function: %q", p.peek().Val)
	}
	return fn, nil
}

// ParseProgram parses decoded top-level declarations (each was one base64
// line). Type declarations are parsed first so their names are known when
// parsing functions, which lets `T{...}` be disambiguated from a block.
func ParseProgram(decls []string) (*Program, error) {
	prog := &Program{}
	structs := map[string]bool{}
	var funcSrcs []string
	for _, src := range decls {
		toks, err := Lex(src)
		if err != nil {
			return nil, err
		}
		if toks[0].Kind == TKeyword && toks[0].Val == "type" {
			td, err := ParseType(src)
			if err != nil {
				return nil, err
			}
			prog.Types = append(prog.Types, td)
			structs[td.Name] = true
		} else if toks[0].Kind == TKeyword && toks[0].Val == "extern" {
			ed, err := ParseExtern(src)
			if err != nil {
				return nil, err
			}
			prog.Externs = append(prog.Externs, ed)
		} else {
			funcSrcs = append(funcSrcs, src)
		}
	}
	// a cstruct is also a first-class MFL struct (int/float fields); synthesize
	// its TypeDecl so MFL code can construct and field-access it.
	for _, ed := range prog.Externs {
		for _, cs := range ed.Structs {
			fields := make([]Field, len(cs.Fields))
			for i, f := range cs.Fields {
				fields[i] = Field{Name: f.Name, Type: ffiMFLType(f.CType)}
			}
			prog.Types = append(prog.Types, &TypeDecl{Name: cs.Name, Fields: fields})
			structs[cs.Name] = true
		}
	}
	for _, src := range funcSrcs {
		fn, err := ParseFuncWith(src, structs)
		if err != nil {
			return nil, err
		}
		prog.Funcs = append(prog.Funcs, fn)
	}
	liftClosures(prog) // closure conversion: lift function literals to top level
	return prog, nil
}

// ParseExtern parses a single extern declaration (foreign C functions).
func ParseExtern(src string) (*ExternDecl, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	p := &Parser{toks: toks}
	ed, err := p.parseExternDecl()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != TEOF {
		return nil, fmt.Errorf("trailing tokens after extern: %q", p.peek().Val)
	}
	return ed, nil
}

// isExternDirective reports whether a word starts an extern-block directive
// (and so cannot be a function's return type).
func isExternDirective(s string) bool {
	return s == "header" || s == "link" || s == "cflags" || s == "fn" || s == "cstruct"
}

// parseExternDecl parses:
//   extern "lib" { header "h.h" link "l" cflags "..." fn Name(t, t) ret ... }
func (p *Parser) parseExternDecl() (*ExternDecl, error) {
	if _, err := p.expect(TKeyword, "extern"); err != nil {
		return nil, err
	}
	lib, err := p.expect(TString, "")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TPunct, "{"); err != nil {
		return nil, err
	}
	ed := &ExternDecl{Lib: lib.Val}
	str := func(dst *string) error {
		s, err := p.expect(TString, "")
		if err != nil {
			return err
		}
		*dst = s.Val
		return nil
	}
	for p.peek().Val != "}" && p.peek().Kind != TEOF {
		kw := p.next()
		switch kw.Val {
		case "header":
			if err := str(&ed.Header); err != nil {
				return nil, err
			}
		case "link":
			var l string
			if err := str(&l); err != nil {
				return nil, err
			}
			ed.Links = append(ed.Links, l)
		case "cflags":
			if err := str(&ed.CFlags); err != nil {
				return nil, err
			}
		case "cstruct":
			name, err := p.expect(TIdent, "")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TPunct, "{"); err != nil {
				return nil, err
			}
			var fields []ExternField
			for p.peek().Val != "}" && p.peek().Kind != TEOF {
				fname, err := p.expect(TIdent, "") // field name, then its C type
				if err != nil {
					return nil, err
				}
				ct := p.next()
				if ct.Kind != TIdent {
					return nil, fmt.Errorf("cstruct %s: expected a C type for field %s, got %q", name.Val, fname.Val, ct.Val)
				}
				fields = append(fields, ExternField{Name: fname.Val, CType: ct.Val})
				for p.peek().Val == "," || p.peek().Val == ";" {
					p.next()
				}
			}
			if _, err := p.expect(TPunct, "}"); err != nil {
				return nil, err
			}
			ed.Structs = append(ed.Structs, ExternStruct{Name: name.Val, Fields: fields})
		case "fn":
			name, err := p.expect(TIdent, "")
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TPunct, "("); err != nil {
				return nil, err
			}
			var params []string
			for p.peek().Val != ")" && p.peek().Kind != TEOF {
				t := p.next() // a scalar FFI type or a cstruct name; validated by the checker
				if t.Kind != TIdent {
					return nil, fmt.Errorf("extern fn %s: expected a parameter type, got %q", name.Val, t.Val)
				}
				params = append(params, t.Val)
				for p.peek().Val == "," {
					p.next()
				}
			}
			if _, err := p.expect(TPunct, ")"); err != nil {
				return nil, err
			}
			ret := ""
			if p.peek().Kind == TIdent && !isExternDirective(p.peek().Val) {
				ret = p.next().Val // an explicit return type; absence means void
			}
			ed.Funcs = append(ed.Funcs, ExternFunc{Name: name.Val, Params: params, Ret: ret})
		default:
			return nil, fmt.Errorf("extern: expected header/link/cflags/cstruct/fn, got %q", kw.Val)
		}
	}
	if _, err := p.expect(TPunct, "}"); err != nil {
		return nil, err
	}
	if len(ed.Funcs) == 0 {
		return nil, fmt.Errorf("extern %q: no fn declarations", ed.Lib)
	}
	return ed, nil
}

// ParseType parses a single decoded struct type declaration.
func ParseType(src string) (*TypeDecl, error) {
	toks, err := Lex(src)
	if err != nil {
		return nil, err
	}
	p := &Parser{toks: toks}
	td, err := p.parseTypeDecl()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != TEOF {
		return nil, fmt.Errorf("trailing tokens after type: %q", p.peek().Val)
	}
	return td, nil
}

// parseTypeDecl parses: type Name struct { field type  field type ... }
func (p *Parser) parseTypeDecl() (*TypeDecl, error) {
	if _, err := p.expect(TKeyword, "type"); err != nil {
		return nil, err
	}
	name, err := p.expect(TIdent, "")
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TKeyword, "struct"); err != nil {
		return nil, err
	}
	if _, err := p.expect(TPunct, "{"); err != nil {
		return nil, err
	}
	var fields []Field
	for p.peek().Val != "}" && p.peek().Kind != TEOF {
		fname, err := p.expect(TIdent, "")
		if err != nil {
			return nil, err
		}
		ftype, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		fields = append(fields, Field{Name: fname.Val, Type: ftype})
		for p.peek().Val == ";" || p.peek().Val == "," {
			p.next()
		}
	}
	if _, err := p.expect(TPunct, "}"); err != nil {
		return nil, err
	}
	return &TypeDecl{Name: name.Val, Fields: fields}, nil
}

// parseTypeName parses a field/element type: int, float, bool, string, a struct
// name, or []elem.
func (p *Parser) parseTypeName() (string, error) {
	if p.peek().Val == "[" {
		p.next()
		if _, err := p.expect(TPunct, "]"); err != nil {
			return "", err
		}
		elem, err := p.parseTypeName()
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	}
	if p.peek().Val == "map" {
		p.next()
		if _, err := p.expect(TPunct, "["); err != nil {
			return "", err
		}
		key, err := p.parseTypeName()
		if err != nil {
			return "", err
		}
		if _, err := p.expect(TPunct, "]"); err != nil {
			return "", err
		}
		val, err := p.parseTypeName()
		if err != nil {
			return "", err
		}
		return "map[" + key + "]" + val, nil
	}
	if p.peek().Val == "chan" {
		p.next()
		elem, err := p.parseTypeName()
		if err != nil {
			return "", err
		}
		return "chan " + elem, nil
	}
	if p.peek().Val == "func" { // a function value; its signature is inferred
		p.next()
		return "func", nil
	}
	t := p.next()
	if t.Kind != TIdent {
		return "", fmt.Errorf("expected a type name, got %q", t.Val)
	}
	return t.Val, nil
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
	variadic := false
	for p.peek().Val != ")" {
		pt, err := p.expect(TIdent, "")
		if err != nil {
			return nil, err
		}
		params = append(params, pt.Val)
		if p.peek().Val == "..." { // variadic: must be the last parameter
			p.next()
			variadic = true
			break
		}
		if p.peek().Val == "," {
			p.next()
		} else {
			break
		}
	}
	if _, err := p.expect(TPunct, ")"); err != nil {
		return nil, err
	}
	// optional named return values: func f(a, b) (q, r) { ... }
	var returns []string
	if p.peek().Val == "(" {
		p.next()
		for p.peek().Val != ")" {
			rt, err := p.expect(TIdent, "")
			if err != nil {
				return nil, err
			}
			returns = append(returns, rt.Val)
			if p.peek().Val == "," {
				p.next()
			} else {
				break
			}
		}
		if _, err := p.expect(TPunct, ")"); err != nil {
			return nil, err
		}
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &FuncDecl{Name: nameTok.Val, Params: params, Returns: returns, Variadic: variadic, Body: body}, nil
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
			vals, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			return &ReturnStmt{Vals: vals}, nil
		case "if":
			return p.parseIf()
		case "while":
			return p.parseWhile()
		case "for":
			return p.parseFor()
		case "arena":
			p.next()
			body, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			return &ArenaStmt{Body: body}, nil
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
	// multi-assign: ident, ident, ... (:=|=) rhs
	if t.Kind == TIdent && p.toks[p.pos+1].Val == "," {
		return p.parseMultiAssign()
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
	if p.peek().Val == "<-" { // channel send: ch <- v
		p.next()
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return &SendStmt{Ch: x, Val: val}, nil
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
		case *FieldAccess:
			return &FieldAssign{Target: lhs, Val: val}, nil
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
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Cond: cond, Body: body}, nil
}

// parseExprList parses one or more comma-separated expressions.
func (p *Parser) parseExprList() ([]Expr, error) {
	var list []Expr
	for {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		list = append(list, e)
		if !p.atPunct(",") {
			break
		}
		p.next()
	}
	return list, nil
}

// parseMultiAssign parses `a, b, ... (:=|=) rhs`.
func (p *Parser) parseMultiAssign() (Stmt, error) {
	var names []string
	for {
		n, err := p.expect(TIdent, "")
		if err != nil {
			return nil, err
		}
		names = append(names, n.Val)
		if p.peek().Val != "," {
			break
		}
		p.next()
	}
	op := p.peek().Val
	if op != ":=" && op != "=" {
		return nil, fmt.Errorf("expected := or = after name list, got %q", op)
	}
	p.next()
	rhs, err := p.parseExprList()
	if err != nil {
		return nil, err
	}
	return &MultiAssign{Names: names, Op: op, Rhs: rhs}, nil
}

// parseRange parses `IDENT [, IDENT] := range EXPR { ... }` (the `for` already
// consumed). The first name is the index/key, the optional second is the value.
func (p *Parser) parseRange() (Stmt, error) {
	key, err := p.expect(TIdent, "")
	if err != nil {
		return nil, err
	}
	val := ""
	if p.peek().Val == "," {
		p.next()
		vt, err := p.expect(TIdent, "")
		if err != nil {
			return nil, err
		}
		val = vt.Val
	}
	if _, err := p.expect(TOp, ":="); err != nil {
		return nil, err
	}
	if _, err := p.expect(TKeyword, "range"); err != nil {
		return nil, err
	}
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &RangeStmt{Key: key.Val, Val: val, X: x, Body: body}, nil
}

// parseFor handles Go's looping forms, desugared onto WhileStmt:
//   for { ... }        infinite loop
//   for cond { ... }   loop while cond
func (p *Parser) parseFor() (Stmt, error) {
	p.next() // for
	if p.peek().Val == "{" {
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		return &WhileStmt{Cond: &BoolLit{Val: true}, Body: body}, nil
	}
	// range header: `for IDENT [, IDENT] := range EXPR`
	if p.peek().Kind == TIdent && (p.toks[p.pos+1].Val == ":=" || p.toks[p.pos+1].Val == ",") {
		return p.parseRange()
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &WhileStmt{Cond: cond, Body: body}, nil
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
	if t.Kind == TOp && t.Val == "<-" { // channel receive
		p.next()
		ch, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Recv{Ch: ch}, nil
	}
	return p.parsePostfix()
}

// parsePostfix parses a primary followed by any number of [index] operators.
func (p *Parser) parsePostfix() (Expr, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.peek().Val == "[" || p.peek().Val == "." || p.peek().Val == "(" {
		switch p.peek().Val {
		case ".":
			p.next()
			name, err := p.expect(TIdent, "")
			if err != nil {
				return nil, err
			}
			x = &FieldAccess{X: x, Name: name.Val}
		case "(":
			args, _, err := p.parseCallArgs()
			if err != nil {
				return nil, err
			}
			x = &CallValue{Fn: x, Args: args}
		default: // "["
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
		case "make":
			return p.parseMake()
		case "func":
			return p.parseFuncLit()
		}
		return nil, fmt.Errorf("unexpected keyword %q", t.Val)
	case TIdent:
		p.next()
		if p.peek().Val == "(" {
			return p.parseCall(t.Val)
		}
		if p.peek().Val == "{" && p.structs[t.Val] {
			return p.parseStructLit(t.Val)
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

// parseMake parses make(chan T) or make(map[K]V).
func (p *Parser) parseMake() (Expr, error) {
	p.next() // make
	if _, err := p.expect(TPunct, "("); err != nil {
		return nil, err
	}
	switch p.peek().Val {
	case "chan":
		p.next()
		elem, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TPunct, ")"); err != nil {
			return nil, err
		}
		return &MakeChan{Elem: elem}, nil
	case "map":
		p.next()
		if _, err := p.expect(TPunct, "["); err != nil {
			return nil, err
		}
		key, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TPunct, "]"); err != nil {
			return nil, err
		}
		val, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TPunct, ")"); err != nil {
			return nil, err
		}
		return &MakeMap{Key: key, Val: val}, nil
	}
	return nil, fmt.Errorf("make: expected chan or map, got %q", p.peek().Val)
}

// parseStructLit parses Point{x: 1, y: 2} (keyed) or Point{1, 2} (positional).
func (p *Parser) parseStructLit(typeName string) (Expr, error) {
	if _, err := p.expect(TPunct, "{"); err != nil {
		return nil, err
	}
	lit := &StructLit{Type: typeName}
	for !p.atPunct("}") && p.peek().Kind != TEOF {
		// keyed?  ident ':' expr
		if p.peek().Kind == TIdent && p.toks[p.pos+1].Val == ":" {
			name := p.next().Val
			p.next() // ':'
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			lit.FieldNames = append(lit.FieldNames, name)
			lit.Vals = append(lit.Vals, val)
		} else {
			val, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			lit.Vals = append(lit.Vals, val)
		}
		if p.atPunct(",") {
			p.next()
		} else {
			break
		}
	}
	if _, err := p.expect(TPunct, "}"); err != nil {
		return nil, err
	}
	if len(lit.FieldNames) != 0 && len(lit.FieldNames) != len(lit.Vals) {
		return nil, fmt.Errorf("struct literal %s mixes keyed and positional fields", typeName)
	}
	return lit, nil
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
	for !p.atPunct("}") && p.peek().Kind != TEOF {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		elems = append(elems, e)
		if p.atPunct(",") {
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
	args, spread, err := p.parseCallArgs()
	if err != nil {
		return nil, err
	}
	return &Call{Callee: callee, Args: args, Spread: spread}, nil
}

// parseCallArgs parses a parenthesized argument list, reporting whether the
// final argument is spread (`expr...`).
// atPunct reports whether the next token is the given punctuation/operator —
// distinct from a string literal that merely has the same value (e.g. ")").
func (p *Parser) atPunct(val string) bool {
	t := p.peek()
	return (t.Kind == TPunct || t.Kind == TOp) && t.Val == val
}

func (p *Parser) parseCallArgs() ([]Expr, bool, error) {
	if _, err := p.expect(TPunct, "("); err != nil {
		return nil, false, err
	}
	var args []Expr
	spread := false
	for !p.atPunct(")") && p.peek().Kind != TEOF {
		a, err := p.parseExpr()
		if err != nil {
			return nil, false, err
		}
		args = append(args, a)
		if p.atPunct("...") { // spread: must be the last argument
			p.next()
			spread = true
			break
		}
		if p.atPunct(",") {
			p.next()
		} else {
			break
		}
	}
	if _, err := p.expect(TPunct, ")"); err != nil {
		return nil, false, err
	}
	return args, spread, nil
}

// parseFuncLit parses an anonymous function: func(a, b) { ... }.
func (p *Parser) parseFuncLit() (Expr, error) {
	p.next() // func
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
	return &FuncLit{Params: params, Body: body}, nil
}
