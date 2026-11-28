package main

import "testing"

// ptr_bytes keeps NULs that bytes(string) and ptr_str would stop at (#759).
func TestPtrBytes(t *testing.T) {
	out, err := RunCaptured(progFromSrc(t, `
func main() {
    p := alloc(4)
    poke_u8(p, 0, 65)
    poke_u8(p, 2, 66)
    b := ptr_bytes(p, 4)
    poke_u8(p, 0, 90)
    free(p)
    println(to_hex(b))
    println(str(len(ptr_bytes(0, 4))) + str(len(ptr_bytes(p, -1))) + str(len(ptr_bytes(p, 0))))
}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "41004200\n000\n" {
		t.Fatalf("got %q", out)
	}
}
