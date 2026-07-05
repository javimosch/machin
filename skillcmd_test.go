package main

import "testing"

func TestResolveSkill(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"machin-start", "machin-start"}, // already-canonical name
		{"start", "machin-start"},        // alias
		{"machin", "machin-start"},       // alias
		{"web", "machin-web"},            // alias
		{"game", "machin-gamedev"},       // alias
		{"bogus", ""},                    // unknown
	}
	for _, c := range cases {
		if got := resolveSkill(c.name); got != c.want {
			t.Errorf("resolveSkill(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}
