package main

import (
	"strings"
	"testing"
)

// An escape the lexer does not know used to lose its backslash silently (#757).
func TestUnknownStringEscapeIsAnError(t *testing.T) {
	for _, e := range []string{`\x41`, `\q`, `\0`, `\u`, `\é`} {
		_, err := Lex(`func main() { s := "a` + e + `b" }`)
		if err == nil || !strings.Contains(err.Error(), "unknown escape "+e[:2]) && !strings.Contains(err.Error(), "unknown escape "+e) {
			t.Errorf("%s: got %v", e, err)
		}
	}
	if _, err := Lex(`func main() { s := "\n\t\r\"\\" }`); err != nil {
		t.Fatalf("supported escapes: %v", err)
	}
}
