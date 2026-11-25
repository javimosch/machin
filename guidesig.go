package main

import (
	"fmt"
	"regexp"
	"strings"
)

// The structured form of a catalog signature (#712). Builtins are not parsed
// from a second table: it is derived from guideBuiltin.Sig, so the two cannot
// disagree.

type guideParam struct {
	Name     string `json:"name,omitempty"`
	Type     string `json:"type"` // a type name, "a|b" for a union, "any"/"T"/"K"/"V" for generic
	Optional bool   `json:"optional,omitempty"`
}

type guideSignature struct {
	Params   []guideParam `json:"params"`
	Variadic bool         `json:"variadic,omitempty"` // the last param repeats, zero or more times
	Returns  []string     `json:"returns"`            // empty = no value; two or more = multi-assign only
}

var namedParamRe = regexp.MustCompile(`^(int|float|string|bool|bytes) ([A-Za-z_]\w*)$`)

// parseGuideSig parses the catalog grammar:
//
//	(p, p[, optional]) -> ret | (r1, r2) | nothing
//
// where p is `type` or `type name`, and `...` alone means any number of any value.
func parseGuideSig(sig string) (guideSignature, error) {
	out := guideSignature{Params: []guideParam{}, Returns: []string{}}
	if !strings.HasPrefix(sig, "(") {
		return out, fmt.Errorf("signature %q does not start with (", sig)
	}
	end := matchParen(sig)
	if end < 0 {
		return out, fmt.Errorf("signature %q has unbalanced parentheses", sig)
	}
	args, rest := sig[1:end], strings.TrimSpace(sig[end+1:])
	if !strings.HasPrefix(rest, "->") {
		return out, fmt.Errorf("signature %q has no ->", sig)
	}
	optional := ""
	if i := strings.Index(args, "[, "); i >= 0 {
		if !strings.HasSuffix(args, "]") {
			return out, fmt.Errorf("signature %q: the optional group must come last", sig)
		}
		args, optional = args[:i], args[i+3:len(args)-1]
	}
	for _, group := range []struct {
		text     string
		optional bool
	}{{args, false}, {optional, true}} {
		for _, p := range splitTopLevel(group.text) {
			if p == "..." {
				out.Variadic = true
				out.Params = append(out.Params, guideParam{Type: "any"})
				continue
			}
			gp := guideParam{Type: p, Optional: group.optional}
			if m := namedParamRe.FindStringSubmatch(p); m != nil {
				gp.Type, gp.Name = m[1], m[2]
			}
			out.Params = append(out.Params, gp)
		}
	}
	ret := strings.TrimSpace(rest[2:])
	if strings.HasPrefix(ret, "(") && matchParen(ret) == len(ret)-1 {
		out.Returns = append(out.Returns, splitTopLevel(ret[1:len(ret)-1])...)
	} else if ret != "" && ret != "void" {
		out.Returns = append(out.Returns, ret)
	}
	return out, nil
}

// matchParen is the index of the ) closing the ( at s[0], or -1.
func matchParen(s string) int {
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitTopLevel splits a list on the commas that are not inside (), [] or {}.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(s[start:]); last != "" {
		out = append(out, last)
	}
	return out
}
