package main

import "testing"

func TestParseMakeMissingParen(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { ch := make chan int }`))
	if err == nil {
		t.Fatal("expected error for make without opening paren")
	}
}

func TestParseMakeChanMissingType(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { ch := make(chan) }`))
	if err == nil {
		t.Fatal("expected error for chan without element type")
	}
}

func TestParseMakeChanMissingCloseParen(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { ch := make(chan int }`))
	if err == nil {
		t.Fatal("expected error for make(chan) without closing paren")
	}
}

func TestParseMakeMapMissingBracket(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { m := make(map string int) }`))
	if err == nil {
		t.Fatal("expected error for map without bracket")
	}
}

func TestParseMakeMapMissingKeyType(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { m := make(map[int) }`))
	if err == nil {
		t.Fatal("expected error for map without key type")
	}
}

func TestParseMakeMapMissingCloseBracket(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { m := make(map[string int) }`))
	if err == nil {
		t.Fatal("expected error for map without closing bracket")
	}
}

func TestParseMakeMapMissingValType(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { m := make(map[string]) }`))
	if err == nil {
		t.Fatal("expected error for map without value type")
	}
}

func TestParseRangeSingleKey(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { for i := range arr { } }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	rs, ok := fn.Body[0].(*RangeStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *RangeStmt", fn.Body[0])
	}
	if rs.Key != "i" || rs.Val != "" {
		t.Errorf("RangeStmt Key/Val = %q/%q, want i/empty", rs.Key, rs.Val)
	}
}

func TestParseRangeDoubleKey(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { for i, v := range arr { } }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	rs, ok := fn.Body[0].(*RangeStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *RangeStmt", fn.Body[0])
	}
	if rs.Key != "i" || rs.Val != "v" {
		t.Errorf("RangeStmt Key/Val = %q/%q, want i/v", rs.Key, rs.Val)
	}
}

func TestParseRangeMissingIdent(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { for := range arr { } }`))
	if err == nil {
		t.Fatal("expected error for range without key ident")
	}
}

func TestParseRangeMissingAssign(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { for i range arr { } }`))
	if err == nil {
		t.Fatal("expected error for range without := operator")
	}
}

func TestParseRangeMissingRangeKeyword(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { for i := arr { } }`))
	if err == nil {
		t.Fatal("expected error for range without range keyword")
	}
}

func TestParseRangeMissingCommaIdent(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { for i, := range arr { } }`))
	if err == nil {
		t.Fatal("expected error for range double-key with missing second ident")
	}
}

func TestParseSliceLitEmpty(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { s := []int{} }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	assign := fn.Body[0].(*AssignStmt)
	sl, ok := assign.Val.(*SliceLit)
	if !ok {
		t.Fatalf("Val = %T, want *SliceLit", assign.Val)
	}
	if sl.Elem != "int" || len(sl.Elems) != 0 {
		t.Errorf("SliceLit Elem/len = %q/%d, want int/0", sl.Elem, len(sl.Elems))
	}
}

func TestParseSliceLitInt(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { s := []int{1, 2, 3} }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	assign := fn.Body[0].(*AssignStmt)
	sl, ok := assign.Val.(*SliceLit)
	if !ok {
		t.Fatalf("Val = %T, want *SliceLit", assign.Val)
	}
	if sl.Elem != "int" || len(sl.Elems) != 3 {
		t.Errorf("SliceLit Elem/len = %q/%d, want int/3", sl.Elem, len(sl.Elems))
	}
}

func TestParseSliceLitString(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { s := []string{"a", "b"} }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	assign := fn.Body[0].(*AssignStmt)
	sl, ok := assign.Val.(*SliceLit)
	if !ok {
		t.Fatalf("Val = %T, want *SliceLit", assign.Val)
	}
	if sl.Elem != "string" || len(sl.Elems) != 2 {
		t.Errorf("SliceLit Elem/len = %q/%d, want string/2", sl.Elem, len(sl.Elems))
	}
}

func TestParseSliceLitFunc(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { s := []func{} }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	assign := fn.Body[0].(*AssignStmt)
	sl, ok := assign.Val.(*SliceLit)
	if !ok {
		t.Fatalf("Val = %T, want *SliceLit", assign.Val)
	}
	if sl.Elem != "func" {
		t.Errorf("SliceLit Elem = %q, want func", sl.Elem)
	}
}

func TestParseSliceLitMissingBracket(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { s := int{} }`))
	if err == nil {
		t.Fatal("expected error for slice without opening bracket")
	}
}

func TestParseSliceLitMissingCloseBracket(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { s := [int{} }`))
	if err == nil {
		t.Fatal("expected error for slice without closing bracket")
	}
}

func TestParseSliceLitBadElemType(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { s := []123{} }`))
	if err == nil {
		t.Fatal("expected error for slice with invalid element type")
	}
}

func TestParseSliceLitMissingBrace(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { s := []int 1, 2 }`))
	if err == nil {
		t.Fatal("expected error for slice without opening brace")
	}
}

func TestParseSliceLitMissingCloseBrace(t *testing.T) {
	_, err := ParseFunc(normalize(`func main() { s := []int{1, 2 }`))
	if err == nil {
		t.Fatal("expected error for slice without closing brace")
	}
}

func TestParseExternSimple(t *testing.T) {
	src := normalize(`extern "m" { fn sqrt(float) float }`)
	ed, err := ParseExtern(src)
	if err != nil {
		t.Fatalf("ParseExtern: unexpected error: %v", err)
	}
	if ed.Lib != "m" {
		t.Errorf("ExternDecl.Lib = %q, want m", ed.Lib)
	}
	if len(ed.Funcs) != 1 || ed.Funcs[0].Name != "sqrt" {
		t.Errorf("ExternDecl.Funcs = %v, want one sqrt function", ed.Funcs)
	}
}

func TestParseExternWithHeader(t *testing.T) {
	src := normalize(`extern "m" { header "math.h" fn sqrt(float) float }`)
	ed, err := ParseExtern(src)
	if err != nil {
		t.Fatalf("ParseExtern: unexpected error: %v", err)
	}
	if ed.Header != "math.h" {
		t.Errorf("ExternDecl.Header = %q, want math.h", ed.Header)
	}
}

func TestParseExternMissingKeyword(t *testing.T) {
	_, err := ParseExtern(normalize(`"m" { fn sqrt(float) float }`))
	if err == nil {
		t.Fatal("expected error for missing extern keyword")
	}
}

func TestParseExternMissingLib(t *testing.T) {
	_, err := ParseExtern(normalize(`extern { fn sqrt(float) float }`))
	if err == nil {
		t.Fatal("expected error for missing library string")
	}
}

func TestParseExternMissingBrace(t *testing.T) {
	_, err := ParseExtern(normalize(`extern "m" fn sqrt(float) float`))
	if err == nil {
		t.Fatal("expected error for missing opening brace")
	}
}

func TestParseExternTrailingTokens(t *testing.T) {
	_, err := ParseExtern(normalize(`extern "m" { fn sqrt(float) float } extra`))
	if err == nil {
		t.Fatal("expected error for trailing tokens after extern")
	}
}
