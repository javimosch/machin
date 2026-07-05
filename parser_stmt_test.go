package main

import (
	"testing"
)

// TestParseIfSimple exercises the basic if statement parsing path
// without else clauses.
func TestParseIfSimple(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { if true { println("yes") } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
}

// TestParseIfElse exercises the if-else statement path.
func TestParseIfElse(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { if false { println("no") } else { println("yes") } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
}

// TestParseIfElseIf exercises the if-else-if chaining path.
func TestParseIfElseIf(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { if false { println("a") } else if true { println("b") } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
}

// TestParseIfMultiElseIf exercises chaining multiple else-if blocks.
func TestParseIfMultiElseIf(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() {
			if false { println("a") }
			else if false { println("b") }
			else if true { println("c") }
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
}

// TestParseIfElseIfElse exercises the full if-else if-else chain.
func TestParseIfElseIfElse(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() {
			if false { println("a") }
			else if false { println("b") }
			else { println("c") }
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
}

// TestParseWhileSimple exercises basic while loop parsing.
func TestParseWhileSimple(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() { i := 0 while i < 10 { i = i + 1 } }`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
}

// TestParseWhileNested exercises nested while loops.
func TestParseWhileNested(t *testing.T) {
	prog, err := ParseProgram([]string{
		`func main() {
			i := 0
			while i < 3 {
				j := 0
				while j < 3 {
					println(j)
					j = j + 1
				}
				i = i + 1
			}
		}`,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog == nil || len(prog.Funcs) != 1 {
		t.Fatalf("expected one function, got %v", prog)
	}
}
