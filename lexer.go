package main

import (
	"fmt"
	"strings"
)

type TokKind int

const (
	TEOF TokKind = iota
	TIdent
	TInt
	TFloat
	TString
	TPunct  // ( ) { } , ;
	TOp     // + - * / % == != < <= > >= && || ! = :=
	TKeyword
)

type Token struct {
	Kind TokKind
	Val  string
	Pos  int
}

var keywords = map[string]bool{
	"func": true, "return": true, "if": true, "else": true,
	"while": true, "for": true, "true": true, "false": true,
	"nil": true, "var": true, "go": true, "type": true, "struct": true,
	"chan": true, "make": true, "map": true, "range": true,
}

type Lexer struct {
	src  string
	pos  int
	toks []Token
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool  { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isAlnum(c byte) bool  { return isAlpha(c) || isDigit(c) }

func Lex(src string) ([]Token, error) {
	l := &Lexer{src: src}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case isDigit(c):
			l.lexNumber()
		case isAlpha(c):
			l.lexIdent()
		case c == '"':
			if err := l.lexString(); err != nil {
				return nil, err
			}
		default:
			if err := l.lexOpOrPunct(); err != nil {
				return nil, err
			}
		}
	}
	l.toks = append(l.toks, Token{Kind: TEOF, Pos: l.pos})
	return l.toks, nil
}

func (l *Lexer) lexNumber() {
	start := l.pos
	isFloat := false
	for l.pos < len(l.src) && (isDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
		if l.src[l.pos] == '.' {
			isFloat = true
		}
		l.pos++
	}
	kind := TInt
	if isFloat {
		kind = TFloat
	}
	l.toks = append(l.toks, Token{Kind: kind, Val: l.src[start:l.pos], Pos: start})
}

func (l *Lexer) lexIdent() {
	start := l.pos
	for l.pos < len(l.src) && isAlnum(l.src[l.pos]) {
		l.pos++
	}
	val := l.src[start:l.pos]
	kind := TIdent
	if keywords[val] {
		kind = TKeyword
	}
	l.toks = append(l.toks, Token{Kind: kind, Val: val, Pos: start})
}

func (l *Lexer) lexString() error {
	start := l.pos
	l.pos++ // skip opening quote
	var sb strings.Builder
	for l.pos < len(l.src) && l.src[l.pos] != '"' {
		c := l.src[l.pos]
		if c == '\\' && l.pos+1 < len(l.src) {
			l.pos++
			switch l.src[l.pos] {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			default:
				sb.WriteByte(l.src[l.pos])
			}
		} else {
			sb.WriteByte(c)
		}
		l.pos++
	}
	if l.pos >= len(l.src) {
		return fmt.Errorf("unterminated string at %d", start)
	}
	l.pos++ // skip closing quote
	l.toks = append(l.toks, Token{Kind: TString, Val: sb.String(), Pos: start})
	return nil
}

func (l *Lexer) lexOpOrPunct() error {
	start := l.pos
	c := l.src[l.pos]
	two := ""
	if l.pos+1 < len(l.src) {
		two = l.src[l.pos : l.pos+2]
	}
	switch two {
	case "==", "!=", "<=", ">=", "&&", "||", ":=", "<-":
		l.pos += 2
		l.toks = append(l.toks, Token{Kind: TOp, Val: two, Pos: start})
		return nil
	}
	switch c {
	case '(', ')', '{', '}', ',', ';', '[', ']', '.', ':':
		l.pos++
		l.toks = append(l.toks, Token{Kind: TPunct, Val: string(c), Pos: start})
		return nil
	case '+', '-', '*', '/', '%', '<', '>', '!', '=':
		l.pos++
		l.toks = append(l.toks, Token{Kind: TOp, Val: string(c), Pos: start})
		return nil
	}
	return fmt.Errorf("unexpected character %q at %d", string(c), start)
}
