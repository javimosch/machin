package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// loadDecls had no direct test — only exercised indirectly through loadMFL.
// Cover its three paths: plain source, packed (base64) source, and a bad
// packed line.
func TestLoadDecls_Plain(t *testing.T) {
	decls, err := loadDecls("func main() { print(1) }\n")
	if err != nil {
		t.Fatalf("loadDecls: %v", err)
	}
	if len(decls) != 1 || !strings.Contains(decls[0], "print") {
		t.Fatalf("got %+v, want one decl containing print", decls)
	}
}

func TestLoadDecls_Packed(t *testing.T) {
	line := base64.StdEncoding.EncodeToString([]byte("func main(){print(1)}"))
	decls, err := loadDecls(line + "\n")
	if err != nil {
		t.Fatalf("loadDecls: %v", err)
	}
	if len(decls) != 1 || decls[0] != "func main(){print(1)}" {
		t.Fatalf("got %+v, want decoded packed decl", decls)
	}
}

func TestLoadDecls_BadPacked(t *testing.T) {
	_, err := loadDecls("not-valid-base64-!!!\n")
	if err == nil {
		t.Fatal("loadDecls should error on an invalid packed line, got nil")
	}
	if !strings.Contains(err.Error(), "not valid packed") {
		t.Fatalf("err = %v, want mention of invalid packed MFL", err)
	}
}

func TestLoadDecls_Empty(t *testing.T) {
	decls, err := loadDecls("")
	if err != nil {
		t.Fatalf("loadDecls empty: %v", err)
	}
	if len(decls) != 0 {
		t.Fatalf("loadDecls empty: got %+v, want empty slice", decls)
	}
}

func TestLoadDecls_WhitespaceOnly(t *testing.T) {
	decls, err := loadDecls("  \n  \n\n")
	if err != nil {
		t.Fatalf("loadDecls whitespace only: %v", err)
	}
	if len(decls) != 0 {
		t.Fatalf("loadDecls whitespace only: got %+v, want empty slice", decls)
	}
}

func TestLoadDecls_MultiplePlain(t *testing.T) {
	decls, err := loadDecls("func f1() {} func f2() {}")
	if err != nil {
		t.Fatalf("loadDecls multiple: %v", err)
	}
	if len(decls) != 2 {
		t.Fatalf("loadDecls multiple: got %d decls, want 2", len(decls))
	}
}

func TestLoadDecls_MultiplePacked(t *testing.T) {
	line1 := base64.StdEncoding.EncodeToString([]byte("func f1(){}"))
	line2 := base64.StdEncoding.EncodeToString([]byte("func f2(){}"))
	decls, err := loadDecls(line1 + "\n" + line2 + "\n")
	if err != nil {
		t.Fatalf("loadDecls multiple packed: %v", err)
	}
	if len(decls) != 2 {
		t.Fatalf("loadDecls multiple packed: got %d decls, want 2", len(decls))
	}
	if decls[0] != "func f1(){}" || decls[1] != "func f2(){}" {
		t.Fatalf("loadDecls multiple packed: got %+v, want decoded decls", decls)
	}
}
