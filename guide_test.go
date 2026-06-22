package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The catalog must be well-formed and self-consistent: version stamped, no
// duplicate builtins, every section populated.
func TestGuideCatalog(t *testing.T) {
	g := machinGuide()
	if g.Version != machinVersion {
		t.Fatalf("guide version %q != machinVersion %q", g.Version, machinVersion)
	}
	if g.Schema == "" || g.Tagline == "" {
		t.Fatal("guide missing schema/tagline")
	}
	if len(g.Builtins) < 40 || len(g.Idioms) == 0 || len(g.Gotchas) == 0 || len(g.Keywords) == 0 {
		t.Fatalf("guide looks under-populated: %d builtins, %d idioms, %d gotchas", len(g.Builtins), len(g.Idioms), len(g.Gotchas))
	}
	seen := map[string]bool{}
	for _, b := range g.Builtins {
		if seen[b.Name] {
			t.Fatalf("duplicate builtin in catalog: %s", b.Name)
		}
		seen[b.Name] = true
		if b.Sig == "" || b.Summary == "" || b.Category == "" {
			t.Fatalf("builtin %q missing sig/summary/category", b.Name)
		}
	}
	// must marshal to JSON (the default agent-facing output)
	if _, err := json.Marshal(g); err != nil {
		t.Fatalf("guide JSON: %v", err)
	}
}

// The catalog version is the toolchain version; it must match the README badge,
// so cutting a release can't silently leave `machin guide` reporting a stale one.
func TestGuideVersionMatchesReadme(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	marker := "version-" + machinVersion + "-blue"
	if !strings.Contains(string(data), marker) {
		t.Fatalf("README badge does not show machinVersion %q (looked for %q)", machinVersion, marker)
	}
}

// No phantoms: every builtin the catalog advertises must be recognized by the
// compiler. A call to an unknown name fails with `undefined function "name"`;
// any other error (arity/type/multi-assign) still means the builtin is real.
func TestGuideBuiltinsRecognized(t *testing.T) {
	for _, b := range machinGuide().Builtins {
		n := argCount(b.Sig)
		args := make([]string, n)
		for i := range args {
			args[i] = `""`
		}
		src := fmt.Sprintf("func main() { %s(%s) }", b.Name, strings.Join(args, ", "))
		fn, err := ParseFunc(normalize(src))
		if err != nil {
			t.Fatalf("builtin %q: parse: %v", b.Name, err)
		}
		_, err = Check(&Program{Funcs: []*FuncDecl{fn}})
		if err != nil && strings.Contains(err.Error(), fmt.Sprintf("undefined function %q", b.Name)) {
			t.Fatalf("catalog lists builtin %q but the compiler does not know it", b.Name)
		}
	}
}

// Every idiom in the guide must be valid, type-correct MFL — so the reference an
// agent copies from can't rot. Compiled through the real encode path (which
// registers struct types first), but not linked/run (network/FFI idioms only
// need to type-check).
func TestGuideIdiomsCompile(t *testing.T) {
	for _, id := range machinGuide().Idioms {
		blocks, err := splitFunctions(id.Code)
		if err != nil {
			t.Fatalf("idiom %q: split: %v", id.Name, err)
		}
		var decls []string
		for _, b := range blocks {
			decls = append(decls, normalize(b))
		}
		prog, err := ParseProgram(decls)
		if err != nil {
			t.Fatalf("idiom %q: parse: %v", id.Name, err)
		}
		if _, err := CompileToC(prog, false); err != nil {
			t.Fatalf("idiom %q: typecheck: %v", id.Name, err)
		}
	}
}

// argCount derives the arity from a signature like "(string, int) -> int".
func argCount(sig string) int {
	open := strings.Index(sig, "(")
	close := strings.Index(sig, ")")
	if open < 0 || close <= open {
		return 0
	}
	inner := strings.TrimSpace(sig[open+1 : close])
	if inner == "" {
		return 0
	}
	if strings.Contains(inner, "...") {
		return 1
	}
	return strings.Count(inner, ",") + 1
}
