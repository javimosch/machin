package main

import "testing"

func TestParseTypeSliceField(t *testing.T) {
	td, err := ParseType(`type Box struct { items []int }`)
	if err != nil {
		t.Fatalf("ParseType: unexpected error: %v", err)
	}
	if len(td.Fields) != 1 || td.Fields[0].Type != "[]int" {
		t.Errorf("Fields = %#v, want a single []int field", td.Fields)
	}
}

func TestParseTypeMapField(t *testing.T) {
	td, err := ParseType(`type Store struct { data map[string]int }`)
	if err != nil {
		t.Fatalf("ParseType: unexpected error: %v", err)
	}
	if len(td.Fields) != 1 || td.Fields[0].Type != "map[string]int" {
		t.Errorf("Fields = %#v, want a single map[string]int field", td.Fields)
	}
}

func TestParseTypeChanField(t *testing.T) {
	td, err := ParseType(`type Pipe struct { c chan int }`)
	if err != nil {
		t.Fatalf("ParseType: unexpected error: %v", err)
	}
	if len(td.Fields) != 1 || td.Fields[0].Type != "chan int" {
		t.Errorf("Fields = %#v, want a single chan int field", td.Fields)
	}
}

func TestParseTypeFuncField(t *testing.T) {
	td, err := ParseType(`type Handler struct { cb func }`)
	if err != nil {
		t.Fatalf("ParseType: unexpected error: %v", err)
	}
	if len(td.Fields) != 1 || td.Fields[0].Type != "func" {
		t.Errorf("Fields = %#v, want a single func field", td.Fields)
	}
}

func TestParseTypeNestedSliceOfMap(t *testing.T) {
	td, err := ParseType(`type Grid struct { rows []map[string]int }`)
	if err != nil {
		t.Fatalf("ParseType: unexpected error: %v", err)
	}
	if len(td.Fields) != 1 || td.Fields[0].Type != "[]map[string]int" {
		t.Errorf("Fields = %#v, want a single []map[string]int field", td.Fields)
	}
}

func TestParseTypeBadFieldTypeToken(t *testing.T) {
	if _, err := ParseType(`type Bad struct { n 123 }`); err == nil {
		t.Fatal("ParseType: expected error for a non-identifier field type, got nil")
	}
}

func TestParseTypeMissingTypeKeyword(t *testing.T) {
	if _, err := ParseType(`Box struct { n int }`); err == nil {
		t.Fatal("ParseType: expected error for missing 'type' keyword, got nil")
	}
}

func TestParseTypeMissingStructKeyword(t *testing.T) {
	if _, err := ParseType(`type Box { n int }`); err == nil {
		t.Fatal("ParseType: expected error for missing 'struct' keyword, got nil")
	}
}

func TestParseTypeMissingOpenBrace(t *testing.T) {
	if _, err := ParseType(`type Box struct n int }`); err == nil {
		t.Fatal("ParseType: expected error for missing '{', got nil")
	}
}

func TestParseTypeFieldCommaSeparated(t *testing.T) {
	td, err := ParseType(`type Point struct { x int, y int }`)
	if err != nil {
		t.Fatalf("ParseType: unexpected error: %v", err)
	}
	if len(td.Fields) != 2 {
		t.Fatalf("Fields = %#v, want 2 fields", td.Fields)
	}
}

func TestParseMakeChan(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { c := make(chan int) }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	as, ok := fn.Body[0].(*AssignStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *AssignStmt", fn.Body[0])
	}
	mc, ok := as.Val.(*MakeChan)
	if !ok || mc.Elem != "int" {
		t.Errorf("Val = %#v, want *MakeChan{Elem: \"int\"}", as.Val)
	}
}

func TestParseMakeMap(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { m := make(map[string]int) }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	as, ok := fn.Body[0].(*AssignStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *AssignStmt", fn.Body[0])
	}
	mm, ok := as.Val.(*MakeMap)
	if !ok || mm.Key != "string" || mm.Val != "int" {
		t.Errorf("Val = %#v, want *MakeMap{Key: \"string\", Val: \"int\"}", as.Val)
	}
}

func TestParseMakeUnsupportedKind(t *testing.T) {
	if _, err := ParseFunc(normalize(`func main() { s := make(int) }`)); err == nil {
		t.Fatal("ParseFunc: expected error for make(int), got nil")
	}
}

// #? ParseType's trailing-tokens check and parseTypeName's error branches
// (slice/map/chan with a malformed or missing element type) had zero test
// coverage; each of these exercises one specific error path.
func TestParseTypeTrailingTokens(t *testing.T) {
	if _, err := ParseType(`type Box struct { n int } extra`); err == nil {
		t.Fatal("ParseType: expected error for trailing tokens, got nil")
	}
}

func TestParseTypeMissingFieldName(t *testing.T) {
	if _, err := ParseType(`type Box struct { 123 int }`); err == nil {
		t.Fatal("ParseType: expected error for a non-identifier field name, got nil")
	}
}

func TestParseTypeMissingCloseBrace(t *testing.T) {
	if _, err := ParseType(`type Box struct { n int`); err == nil {
		t.Fatal("ParseType: expected error for missing closing '}', got nil")
	}
}

func TestParseTypeSliceMissingCloseBracket(t *testing.T) {
	if _, err := ParseType(`type Box struct { items [int }`); err == nil {
		t.Fatal("ParseType: expected error for slice type missing ']', got nil")
	}
}

func TestParseTypeSliceMissingElemType(t *testing.T) {
	if _, err := ParseType(`type Box struct { items [] }`); err == nil {
		t.Fatal("ParseType: expected error for slice type missing element type, got nil")
	}
}

func TestParseTypeMapMissingOpenBracket(t *testing.T) {
	if _, err := ParseType(`type Store struct { data map string]int }`); err == nil {
		t.Fatal("ParseType: expected error for map type missing '[', got nil")
	}
}

func TestParseTypeMapMissingKeyType(t *testing.T) {
	if _, err := ParseType(`type Store struct { data map[]int }`); err == nil {
		t.Fatal("ParseType: expected error for map type missing key type, got nil")
	}
}

func TestParseTypeMapMissingCloseBracket(t *testing.T) {
	if _, err := ParseType(`type Store struct { data map[string int }`); err == nil {
		t.Fatal("ParseType: expected error for map type missing ']', got nil")
	}
}

func TestParseTypeMapMissingValType(t *testing.T) {
	if _, err := ParseType(`type Store struct { data map[string] }`); err == nil {
		t.Fatal("ParseType: expected error for map type missing value type, got nil")
	}
}

func TestParseTypeChanMissingElemType(t *testing.T) {
	if _, err := ParseType(`type Pipe struct { c chan }`); err == nil {
		t.Fatal("ParseType: expected error for chan type missing element type, got nil")
	}
}
