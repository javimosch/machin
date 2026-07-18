package main

import (
	"strings"
	"testing"
)

// A bare integer literal must be emitted as a 64-bit C constant (an `LL` suffix):
// 30 * 86400000 = 2_592_000_000 exceeds the int32 max, so without the suffix the C
// multiplication overflowed (int * int) to a negative/garbage value before widening
// to int64_t. A machin int is 64-bit, so the C constant must be too.
func TestIntLitNo32BitOverflow(t *testing.T) {
	out, _ := buildRun(t, `func main() { println(str(30 * 86400000)) }`)
	if strings.TrimSpace(out) != "2592000000" {
		t.Fatalf("30 * 86400000 = %q, want 2592000000 (32-bit overflow?)", out)
	}
}

// keys() must return COPIES of the string keys: a retained keys() result has to
// stay valid even after a key is deleted (its entry freed) and the freed slot is
// reused by later inserts. The old code handed out the entry's own malloc'd key
// pointer, so a retained slice read freed (then reused) memory. Summing lengths is
// order-independent, so this is robust to bucket iteration order.
func TestMapKeysRetainedAfterDelete(t *testing.T) {
	src := `func main() {
		m := make(map[string]int)
		m["alpha"] = 1  m["bravo"] = 2  m["charlie"] = 3
		ks := keys(m)
		delete(m, "bravo")
		m["delta-padding-xx"] = 4  m["echo-padding-yyy"] = 5
		total := 0
		i := 0
		for i < len(ks) { total = total + len(ks[i])  i = i + 1 }
		println(str(total))
	}`
	out, _ := buildRun(t, src)
	// len("alpha")+len("bravo")+len("charlie") = 5+5+7 = 17
	if strings.TrimSpace(out) != "17" {
		t.Fatalf("retained keys() corrupted after delete+reinsert: got %q, want 17", out)
	}
}
