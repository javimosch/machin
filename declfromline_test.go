package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDeclFromLinePlainText(t *testing.T) {
	line := "func main() { println(\"hi\") }"
	got, err := declFromLine(line)
	if err != nil {
		t.Fatalf("declFromLine(%q) error = %v", line, err)
	}
	if got != line {
		t.Fatalf("declFromLine(%q) = %q, want unchanged", line, got)
	}
}

func TestDeclFromLinePacked(t *testing.T) {
	src := "func f() { return 1 }"
	packed := base64.StdEncoding.EncodeToString([]byte(src))
	got, err := declFromLine(packed)
	if err != nil {
		t.Fatalf("declFromLine(%q) error = %v", packed, err)
	}
	if got != src {
		t.Fatalf("declFromLine(%q) = %q, want %q", packed, got, src)
	}
}

func TestDeclFromLineInvalid(t *testing.T) {
	_, err := declFromLine("not-valid-base64!!!")
	if err == nil {
		t.Fatalf("declFromLine(invalid) expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "neither plain MFL nor base64") {
		t.Fatalf("declFromLine(invalid) error = %v, want mention of plain/base64", err)
	}
}

func TestDeclFromLineEmptyString(t *testing.T) {
	_, err := declFromLine("")
	if err == nil {
		t.Fatalf("declFromLine(empty) expected an error, got nil")
	}
}
