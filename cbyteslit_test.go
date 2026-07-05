package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestCBytesLiteralEmpty(t *testing.T) {
	got := cBytesLiteral("blob", nil)
	want := "const unsigned char blob[] = {\n\n};\nconst unsigned long blob_len = 0UL;\n"
	if got != want {
		t.Fatalf("cBytesLiteral(empty) = %q, want %q", got, want)
	}
}

func TestCBytesLiteralContents(t *testing.T) {
	data := []byte{0x00, 0x01, 0xff, 0x10}
	got := cBytesLiteral("blob", data)

	if !strings.HasPrefix(got, "const unsigned char blob[] = {\n") {
		t.Fatalf("cBytesLiteral missing array header, got %q", got)
	}
	if !strings.Contains(got, "0x00,0x01,0xff,0x10,") {
		t.Fatalf("cBytesLiteral missing expected byte sequence, got %q", got)
	}
	wantLen := fmt.Sprintf("const unsigned long blob_len = %dUL;\n", len(data))
	if !strings.HasSuffix(got, wantLen) {
		t.Fatalf("cBytesLiteral(%v) length trailer = %q, want suffix %q", data, got, wantLen)
	}
}

// Every 20th byte (indices 19, 39, ...) ends a line, so 21 bytes must wrap
// onto a second line inside the array body.
func TestCBytesLiteralLineWrap(t *testing.T) {
	data := make([]byte, 21)
	got := cBytesLiteral("blob", data)

	body := strings.TrimSuffix(strings.TrimPrefix(got, "const unsigned char blob[] = {\n"), "\n};\nconst unsigned long blob_len = 21UL;\n")
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("cBytesLiteral(21 bytes) body has %d lines, want 2: %q", len(lines), body)
	}
	if got := strings.Count(lines[0], "0x"); got != 20 {
		t.Fatalf("first line has %d bytes, want 20: %q", got, lines[0])
	}
	if got := strings.Count(lines[1], "0x"); got != 1 {
		t.Fatalf("second line has %d bytes, want 1: %q", got, lines[1])
	}
}
