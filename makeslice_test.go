package main

import "testing"

func TestParseMakeSliceLen(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { s := make([]int, 10) }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	as, ok := fn.Body[0].(*AssignStmt)
	if !ok {
		t.Fatalf("Body[0] = %T, want *AssignStmt", fn.Body[0])
	}
	ms, ok := as.Val.(*MakeSlice)
	if !ok || ms.Elem != "int" || ms.Cap != nil {
		t.Errorf("Val = %#v, want *MakeSlice{Elem: \"int\", Cap: nil}", as.Val)
	}
}

func TestParseMakeSliceLenCap(t *testing.T) {
	fn, err := ParseFunc(normalize(`func main() { s := make([]int, 0, 20) }`))
	if err != nil {
		t.Fatalf("ParseFunc: unexpected error: %v", err)
	}
	as := fn.Body[0].(*AssignStmt)
	ms := as.Val.(*MakeSlice)
	if ms.Elem != "int" || ms.Cap == nil {
		t.Fatalf("Val = %#v, want *MakeSlice with Elem=\"int\" and Cap", as.Val)
	}
}

func TestParseMakeSliceMissingLength(t *testing.T) {
	if _, err := ParseFunc(normalize(`func main() { s := make([]int) }`)); err == nil {
		t.Fatal("ParseFunc: expected error for make([]int) without length, got nil")
	}
}

func TestMakeSliceLen(t *testing.T) {
	got := runNative(t, `func main() { s := make([]int, 3) println(len(s), s[0], s[1], s[2]) }`)
	if got != "3 0 0 0\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMakeSliceLenCap(t *testing.T) {
	got := runNative(t, `func main() { s := make([]int, 0, 4) s = append(s, 1) s = append(s, 2) println(len(s), s[0], s[1]) }`)
	if got != "2 1 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMakeSliceString(t *testing.T) {
	got := runNative(t, `func main() { s := make([]string, 2) println(len(s), s[0]) }`)
	if got != "2 \n" {
		t.Fatalf("got %q", got)
	}
}

func TestMakeSliceFloat(t *testing.T) {
	got := runNative(t, `func main() { s := make([]float, 3) s[1] = 2.5 s = append(s, 3.5) println(s[1], s[3]) }`)
	if got != "2.5 3.5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMakeSliceNested(t *testing.T) {
	got := runNative(t, `func main() { s := make([][]int, 2) println(len(s)) }`)
	if got != "2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMakeSliceSieveCapacity(t *testing.T) {
	// Regression: a 10M-element sieve built with a capacity hint should run
	// in one allocation and produce the correct prime count.
	got := runNative(t, `
func main() {
    n := 10000000
    sieve := make([]int, 0, n+1)
    i := 0
    while i <= n { sieve = append(sieve, 1)  i = i+1 }
    sieve[0] = 0
    sieve[1] = 0
    p := 2
    while p*p <= n {
        if sieve[p] == 1 {
            m := p*p
            while m <= n { sieve[m] = 0  m = m+p }
        }
        p = p+1
    }
    count := 0
    k := 0
    while k <= n { count = count + sieve[k]  k = k+1 }
    println(count)
}`)
	if got != "664579\n" {
		t.Fatalf("sieve got %q, want \"664579\\n\"", got)
	}
}
