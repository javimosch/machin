package main

import "testing"

func TestCBytesLiteral(t *testing.T) {
	out := cBytesLiteral("foo", []byte{0x00, 0x01, 0xff})

	const wantArray = "const unsigned char foo[] = {\n  0x00,0x01,0xff,\n};\n"
	if got := out[:len(wantArray)]; got != wantArray {
		t.Errorf("array body = %q, want %q", got, wantArray)
	}

	const wantLen = "const unsigned long foo_len = 3UL;\n"
	if got := out[len(wantArray):]; got != wantLen {
		t.Errorf("length decl = %q, want %q", got, wantLen)
	}
}
