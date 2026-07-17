package main

import "testing"

// TestMapPartOnMapNode covers MapKeyKind, MapKeyCType, and MapValCType
// against a genuinely map-typed node (the KMap branch of mapPart).
func TestMapPartOnMapNode(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { m := make(map[string]int) m["a"] = 1 for k, v := range m { println(k) println(v) } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	var inst string
	var m Expr
	for _, r := range c.Reps() {
		if c.SrcFunc(r).Name != "main" {
			continue
		}
		inst = r
		for _, stmt := range c.SrcFunc(r).Body {
			if rs, ok := stmt.(*RangeStmt); ok {
				m = rs.X
			}
		}
	}
	if inst == "" || m == nil {
		t.Fatal("no main instance / range base node found")
	}

	if got := c.MapKeyKind(inst, m); got != KString {
		t.Errorf("MapKeyKind(m) = %v, want %v", got, KString)
	}
	if got := c.MapKeyCType(inst, m); got != "char*" {
		t.Errorf("MapKeyCType(m) = %q, want %q", got, "char*")
	}
	if got := c.MapValCType(inst, m); got != "int64_t" {
		t.Errorf("MapValCType(m) = %q, want %q", got, "int64_t")
	}
}

// TestMapPartFallbackOnNonMap covers the slot<0 default-value fallback
// branch of mapPart (KInt/"int64_t") when the queried node isn't a map.
func TestMapPartFallbackOnNonMap(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { n := 5 println(n) }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, err := Check(prog)
	if err != nil {
		t.Fatalf("typecheck: %v", err)
	}

	var inst string
	var n Expr
	for _, r := range c.Reps() {
		if c.SrcFunc(r).Name != "main" {
			continue
		}
		inst = r
		for _, stmt := range c.SrcFunc(r).Body {
			if as, ok := stmt.(*AssignStmt); ok && as.Name == "n" {
				n = as.Val
			}
		}
	}
	if inst == "" || n == nil {
		t.Fatal("no main instance / assign node found")
	}

	if got := c.MapKeyKind(inst, n); got != KInt {
		t.Errorf("MapKeyKind(n) fallback = %v, want %v", got, KInt)
	}
	if got := c.MapKeyCType(inst, n); got != "int64_t" {
		t.Errorf("MapKeyCType(n) fallback = %q, want %q", got, "int64_t")
	}
	if got := c.MapValCType(inst, n); got != "int64_t" {
		t.Errorf("MapValCType(n) fallback = %q, want %q", got, "int64_t")
	}
}
