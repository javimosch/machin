package main

import "testing"

// classifyParse/classifyCheck route checker messages to stable category tags
// (used by `machin check --json`); most branches were previously untested.
func TestClassifyParseBranches(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{`unexpected token "}"`, "parse-unexpected-token"},
		{`expected ")" but got "{"`, "parse-expected"},
		{"unterminated string literal", "parse-unterminated-string"},
		{"unbalanced braces near: func f(", "parse-unbalanced-braces"},
		{"something else entirely", "parse-error"},
		{"", "parse-error"},
	}
	for _, c := range cases {
		if got := classifyParse(c.msg); got != c.want {
			t.Errorf("classifyParse(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

func TestClassifyCheckBranches(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"type mismatch: expected int, got string", "type-mismatch"},
		{`function "len" shadows the builtin`, "shadows-builtin"},
		{"no main function found", "no-main"},
		{`duplicate type "Point"`, "duplicate-type"},
		{`duplicate function "main"`, "duplicate-function"},
		{`struct "Point" has no field "z"`, "undefined-field"},
		{`undefined variable "x"`, "undefined-name"},
		{`unknown identifier "y"`, "undefined-name"},
		{`"z" is not defined`, "undefined-name"},
		{`function "f" expects 2 arguments`, "arity-mismatch"},
		{`wrong number of argument for "f"`, "arity-mismatch"},
		{`arity mismatch for "f"`, "arity-mismatch"},
		// The wording the type checker actually emits (types.go): none of these
		// contain "argument"/"expects"/"arity", so they used to leak as "type-error".
		{"main: expected 2 args, got 3", "arity-mismatch"},
		{"len: 1 arg", "arity-mismatch"},
		{"append: 2 args", "arity-mismatch"},
		{"substr: 3 args (string, start, end)", "arity-mismatch"},
		{"ed25519_verify: 3 args (pub, msg, sig — all bytes)", "arity-mismatch"},
		// A struct field-count message must still classify as undefined-field, not
		// arity (the "field" case is intentionally checked before the arity case).
		{`struct "P": expected 2 fields, got 3`, "undefined-field"},
		{"unsupported construct in this context", "unsupported-construct"},
		{"totally unrecognized checker message", "type-error"},
		{"", "type-error"},
	}
	for _, c := range cases {
		if got := classifyCheck(c.msg); got != c.want {
			t.Errorf("classifyCheck(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}
