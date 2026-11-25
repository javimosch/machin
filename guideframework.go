package main

import (
	"fmt"
	"sort"
	"strings"
)

// `machin guide --framework <name>` (#712): the public surface of an embedded
// framework module. Nothing here is hand-listed — names, parameters and structs
// come from the module's source, and types from the checker run over it — so it
// cannot drift from the framework it describes.

// frameworkRequires are the modules a module must be composed after.
var frameworkRequires = map[string][]string{
	"caldav": {"xml"},
	"mongo":  {"bson"},
	"router": {"reactive"},
	"sso":    {"machweb"},
	"ws":     {"machweb"},
}

// frameworkPrograms are embedded demo apps, not libraries to compose against.
var frameworkPrograms = map[string]bool{"example": true, "router_app": true}

type frameworkField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type frameworkType struct {
	Name   string           `json:"name"`
	Fields []frameworkField `json:"fields"`
}

type frameworkFunc struct {
	Name    string `json:"name"`
	Sig     string `json:"sig"`
	Summary string `json:"summary,omitempty"`
	guideSignature
}

type frameworkCatalog struct {
	Version  string   `json:"version"`
	Schema   string   `json:"schema"`
	Module   string   `json:"module"`
	Compose  string   `json:"compose"`
	Requires []string `json:"requires"`
	// Typed is false when the module cannot be typed without a calling program;
	// every param and return type is then "any" and TypeError says why.
	Typed     bool            `json:"typed"`
	TypeError string          `json:"typeError,omitempty"`
	Types     []frameworkType `json:"types"`
	Functions []frameworkFunc `json:"functions"`
}

// guideFrameworks names the modules `--framework` accepts.
func guideFrameworks() []string {
	var out []string
	for _, m := range frameworkModules() {
		name, isSrc := strings.CutSuffix(m, ".src")
		if isSrc && !frameworkPrograms[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func frameworkSource(module string) (string, error) {
	data, err := frameworkFS.ReadFile("framework/" + module + ".src")
	return string(data), err
}

func parseModules(modules []string) (*Program, error) {
	var combined strings.Builder
	for _, m := range modules {
		src, err := frameworkSource(m)
		if err != nil {
			return nil, err
		}
		combined.WriteString(src)
		combined.WriteByte('\n')
	}
	blocks, err := splitFunctions(combined.String())
	if err != nil {
		return nil, err
	}
	for i := range blocks {
		blocks[i] = normalize(blocks[i])
	}
	return ParseProgram(blocks)
}

func describeFramework(module string) (frameworkCatalog, error) {
	var cat frameworkCatalog
	known := false
	for _, m := range guideFrameworks() {
		known = known || m == module
	}
	if !known {
		return cat, fmt.Errorf("unknown framework %q — available: %s", module, strings.Join(guideFrameworks(), ", "))
	}
	requires := append([]string{}, frameworkRequires[module]...)
	var paths []string
	for _, m := range append(append([]string{}, requires...), module) {
		paths = append(paths, "framework/"+m+".src")
	}
	cat = frameworkCatalog{
		Version: machinVersion, Schema: "machin.framework/v1", Module: module,
		Compose:  "machin encode " + strings.Join(paths, " ") + " app.src > app.mfl",
		Requires: requires, Types: []frameworkType{}, Functions: []frameworkFunc{},
	}

	// what this module itself declares (the required ones are described on their own)
	src, err := frameworkSource(module)
	if err != nil {
		return cat, err
	}
	ownTypes, ownFuncs := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(src, "\n") {
		f := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '(' || r == '{' })
		if len(f) >= 2 && f[0] == "type" {
			ownTypes[f[1]] = true
		}
		if len(f) >= 2 && f[0] == "func" {
			ownFuncs[f[1]] = true
		}
	}
	prog, err := parseModules(append(append([]string{}, requires...), module))
	if err != nil {
		return cat, fmt.Errorf("framework %s: %w", module, err)
	}

	checker, terr := checkSurface(prog)
	cat.Typed = terr == nil
	if terr != nil {
		cat.TypeError = terr.Error()
	}

	for _, td := range prog.Types {
		if !ownTypes[td.Name] {
			continue
		}
		ft := frameworkType{Name: td.Name, Fields: []frameworkField{}}
		for _, f := range td.Fields {
			ft.Fields = append(ft.Fields, frameworkField{Name: f.Name, Type: f.Type})
		}
		cat.Types = append(cat.Types, ft)
	}
	docs := docComments(src)
	for _, fn := range prog.Funcs {
		if fn.IsLambda || !ownFuncs[fn.Name] || strings.HasPrefix(fn.Name, "_") {
			continue
		}
		sig := guideSignature{Params: []guideParam{}, Returns: []string{}, Variadic: fn.Variadic}
		var params, rets []int
		if terr == nil {
			params, rets = checker.surfaceSlots(fn.Name)
		}
		var shown []string
		for i, p := range fn.Params {
			t := "any"
			if i < len(params) {
				t = checker.surfaceType(params[i])
			}
			sig.Params = append(sig.Params, guideParam{Name: p, Type: t})
			shown = append(shown, p+" "+t)
		}
		for i := 0; i < funcArity(fn); i++ {
			t := "any"
			if i < len(rets) {
				t = checker.surfaceType(rets[i])
			}
			sig.Returns = append(sig.Returns, t)
		}
		text := "(" + strings.Join(shown, ", ") + ") ->"
		switch len(sig.Returns) {
		case 0:
		case 1:
			text += " " + sig.Returns[0]
		default:
			text += " (" + strings.Join(sig.Returns, ", ") + ")"
		}
		cat.Functions = append(cat.Functions, frameworkFunc{Name: fn.Name, Sig: text, Summary: docs[fn.Name], guideSignature: sig})
	}
	return cat, nil
}

// docComments maps each function to the first paragraph of the // block
// directly above it.
func docComments(src string) map[string]string {
	out := map[string]string{}
	var block []string
	done := false
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if c, ok := strings.CutPrefix(t, "//"); ok {
			if c = strings.TrimSpace(c); c == "" {
				done = len(block) > 0
			} else if !done {
				block = append(block, c)
			}
			continue
		}
		if rest, ok := strings.CutPrefix(t, "func "); ok && len(block) > 0 {
			if i := strings.IndexByte(rest, '('); i > 0 {
				out[strings.TrimSpace(rest[:i])] = strings.Join(block, " ")
			}
		}
		block, done = nil, false
	}
	return out
}

// surfaceSlots returns the param and return slots of a function's first instance.
func (c *Checker) surfaceSlots(name string) (params, rets []int) {
	for _, inst := range c.instOrder {
		if c.instFn[inst].Name == name {
			return c.funcParam[inst], c.funcRets[inst]
		}
	}
	return nil, nil
}

// surfaceType renders a slot for the catalog: "any" where nothing in the module
// constrains it (the checker would default that to int, which would be a lie).
func (c *Checker) surfaceType(slot int) string {
	r := c.find(slot)
	if c.free[r] {
		return "any"
	}
	switch c.kind[r] {
	case KSlice:
		return "[]" + c.surfaceType(c.elem[r])
	case KChan:
		return "chan " + c.surfaceType(c.elem[r])
	case KMap:
		return "map[" + c.surfaceType(c.mkey[r]) + "]" + c.surfaceType(c.mval[r])
	case KFunc:
		sig := c.fsig[r]
		if sig == nil {
			return "func"
		}
		var ps []string
		for _, p := range sig.params {
			ps = append(ps, c.surfaceType(p))
		}
		s := "func(" + strings.Join(ps, ", ") + ")"
		if sig.ret >= 0 && c.kind[c.find(sig.ret)] != KVoid {
			s += " " + c.surfaceType(sig.ret)
		}
		return s
	}
	return c.typeStringSlot(r)
}

// callersFirst orders functions so each comes before the ones it calls (reverse
// post-order of the call graph, cycles broken at the first visit). Rooting in
// that order lets a helper be typed by a real call site instead of in isolation.
func callersFirst(funcs []*FuncDecl) []*FuncDecl {
	byName := map[string]*FuncDecl{}
	for _, fn := range funcs {
		byName[fn.Name] = fn
	}
	seen := map[string]bool{}
	var post []*FuncDecl
	var visit func(fn *FuncDecl)
	visit = func(fn *FuncDecl) {
		if seen[fn.Name] {
			return
		}
		seen[fn.Name] = true
		for _, callee := range sortedKeys(freeIdents(fn.Body, fn.Params)) {
			if next := byName[callee]; next != nil {
				visit(next)
			}
		}
		post = append(post, fn)
	}
	for _, fn := range funcs {
		visit(fn)
	}
	for i, j := 0, len(post)-1; i < j; i, j = i+1, j-1 {
		post[i], post[j] = post[j], post[i]
	}
	return post
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func renderFrameworkText(c frameworkCatalog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "machin %s — framework %s\n  compose: %s\n", c.Version, c.Module, c.Compose)
	if !c.Typed {
		fmt.Fprintf(&b, "  untyped: %s\n", c.TypeError)
	}
	if len(c.Types) > 0 {
		b.WriteString("\nTYPES\n")
		for _, t := range c.Types {
			var fs []string
			for _, f := range t.Fields {
				fs = append(fs, f.Name+" "+f.Type)
			}
			fmt.Fprintf(&b, "  %s { %s }\n", t.Name, strings.Join(fs, "; "))
		}
	}
	b.WriteString("\nFUNCTIONS\n")
	for _, f := range c.Functions {
		fmt.Fprintf(&b, "  %s %s\n", f.Name, f.Sig)
		if f.Summary != "" {
			fmt.Fprintf(&b, "      %s\n", f.Summary)
		}
	}
	return b.String()
}
