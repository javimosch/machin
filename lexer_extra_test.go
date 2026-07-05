package main

import "testing"

func TestLexBasicTokens(t *testing.T) {
	toks, err := Lex("func add(a, b) { return a + b }")
	if err != nil {
		t.Fatalf("Lex returned error: %v", err)
	}

	want := []Token{
		{Kind: TKeyword, Val: "func"},
		{Kind: TIdent, Val: "add"},
		{Kind: TPunct, Val: "("},
		{Kind: TIdent, Val: "a"},
		{Kind: TPunct, Val: ","},
		{Kind: TIdent, Val: "b"},
		{Kind: TPunct, Val: ")"},
		{Kind: TPunct, Val: "{"},
		{Kind: TKeyword, Val: "return"},
		{Kind: TIdent, Val: "a"},
		{Kind: TOp, Val: "+"},
		{Kind: TIdent, Val: "b"},
		{Kind: TPunct, Val: "}"},
		{Kind: TEOF, Val: ""},
	}

	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i].Kind != w.Kind || toks[i].Val != w.Val {
			t.Errorf("token %d = %+v, want Kind=%v Val=%q", i, toks[i], w.Kind, w.Val)
		}
	}
}
