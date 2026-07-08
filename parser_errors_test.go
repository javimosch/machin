// parser_errors_test.go — drives parser.go error returns via deliberately
// malformed MFL fragments. Only fragments that the parser MUST reject are
// included (fragments the parser happens to accept don't drive an error
// branch — listing them would only hide regressions behind a permissive
// parser).
package main

import "testing"

// TestParseGlobalErrors covers parseGlobal error returns — only inputs we
// know the parser rejects (parseGlobal is permissive about RHS shapes like
// "Nope{}" because type info is not available at parse time).
func TestParseGlobalErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"missing-eq", `var x 1`},                  // no `=`
		{"empty-rhs", `var x = `},                  // missing expression
		{"two-rhs-tokens", `var x = 1 2`},          // two consecutive rhs literals
		{"lhs-not-ident", `var 1 = x`},             // LHS must be an identifier
		{"token-junk", `var x = 1 @ junk`},         // non-expression token
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseGlobalWith(c.src, map[string]bool{})
			if err == nil {
				t.Fatalf("expected parse error for %q, got nil", c.src)
			}
		})
	}
}

// TestParseFuncErrors drives parseFuncDecl error returns.
func TestParseFuncErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"missing-name", `func () {1}`},
		{"stray-junk", `func foo();{1}`},
		{"export-nofunc", `export var x = 1`},
		{"bad-paren-pos", `func foo){1}`},
		{"unbalanced-br-only", `func foo(){x:=1`}, // missing `}`
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseFuncWith(c.src, map[string]bool{})
			if err == nil {
				t.Fatalf("expected error for %q, got nil", c.src)
			}
		})
	}
}

// TestParseSelectErrors drives parseSelect error returns with malformed
// select statements. Only fragments the parser MUST reject are included.
func TestParseSelectErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"no-arrow-rx", `func main(){c := make(chan int) select { case x: } }`}, // case w/o `<-`
		{"stray-semi", `func main(){c := make(chan int) select ; case <-c: } }`},
		{"missing-brace", `func main(){c := make(chan int) select case <-c: }`},
		{"double-arrow-misuse", `func main(){c := make(chan int) select { case <- -: } }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseProgram([]string{c.src}); err == nil {
				t.Fatalf("expected parse error for %q, got nil", c.src)
			}
		})
	}
}

// TestParseMakeErrors drives parseMake error returns with malformed
// `make(...)` expressions.
func TestParseMakeErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"no-args", `func main(){ make() }`},
		{"non-type-first", `func main(){ make(1) }`},
		{"size-string", `func main(){ make([]int, "x") }`},
		{"chan-size-string", `func main(){ make(chan int, "x") }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseProgram([]string{c.src}); err == nil {
				t.Fatalf("expected parse error for %q, got nil", c.src)
			}
		})
	}
}

// TestParseExternErrors drives parseExtern / parseExternDecl error returns.
func TestParseExternErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"missing-library", `extern {}`},                        // no lib name in quotes
		{"unknown-directive", `extern "c" { bogus }`},            // not header/link/cflags/cstruct/fn
		{"bad-field-type", `extern "c" { cstruct Bad { x nope } }`}, // unknown field type
		{"dup-header", `extern "c" { header "a.h" header "b.h" }`},
		{"dup-cstruct", `extern "c" { cstruct A { x int } cstruct A { y int } }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseProgram([]string{c.src}); err == nil {
				t.Fatalf("expected parse error for %q, got nil", c.src)
			}
		})
	}
}

// TestParseTypeErrors drives parseType error returns.
func TestParseTypeErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"missing-struct-keyword", `type X { a int }`},
		{"missing-field-type", `type Y struct { a }`},
		{"no-body", `type Q`},
		{"nested-missing-keyword", `type R { struct { x int } }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseProgram([]string{c.src}); err == nil {
				t.Fatalf("expected parse error for %q, got nil", c.src)
			}
		})
	}
}

// TestParseProgramEmpty covers parseProgram on nil/empty inputs.
func TestParseProgramEmpty(t *testing.T) {
	// Both nil and empty slice may produce nil Program; either is acceptable
	// for coverage as long as the function doesn't panic.
	_, errNil := ParseProgram(nil)
	_, errEmpty := ParseProgram([]string{})
	if errNil == nil && errEmpty == nil {
		t.Log("ParseProgram accepts empty input (no error) — coverage intent: panic-avoidance")
	}
}
