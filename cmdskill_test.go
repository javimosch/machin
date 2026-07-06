package main

import (
	"os"
	"path/filepath"
	"testing"
)

// cmdSkill (the `machin skill` subcommand dispatcher) had no direct test
// coverage: default-to-list, "show", "install", and the unknown-subcommand
// error path.

func withSilencedStdout(t *testing.T, fn func() error) error {
	t.Helper()
	old := os.Stdout
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	defer func() {
		os.Stdout = old
		if devnull != nil {
			devnull.Close()
		}
	}()
	os.Stdout = devnull
	return fn()
}

func TestCmdSkillDefaultsToList(t *testing.T) {
	err := withSilencedStdout(t, func() error { return cmdSkill(nil) })
	if err != nil {
		t.Fatalf("cmdSkill with no args should list, got: %v", err)
	}
}

func TestCmdSkillShowKnown(t *testing.T) {
	err := withSilencedStdout(t, func() error { return cmdSkill([]string{"show", "machin-start"}) })
	if err != nil {
		t.Fatalf("cmdSkill show machin-start: %v", err)
	}
}

func TestCmdSkillShowAlias(t *testing.T) {
	err := withSilencedStdout(t, func() error { return cmdSkill([]string{"show", "web"}) })
	if err != nil {
		t.Fatalf("cmdSkill show web (alias): %v", err)
	}
}

func TestCmdSkillShowUnknownErrors(t *testing.T) {
	err := withSilencedStdout(t, func() error { return cmdSkill([]string{"show", "nope"}) })
	if err == nil {
		t.Fatal("expected an error for an unknown skill name")
	}
}

func TestCmdSkillShowMissingNameErrors(t *testing.T) {
	err := withSilencedStdout(t, func() error { return cmdSkill([]string{"show"}) })
	if err == nil {
		t.Fatal("expected an error when show is given no name")
	}
}

func TestCmdSkillInstallDispatches(t *testing.T) {
	dir := t.TempDir()
	err := withSilencedStdout(t, func() error {
		return cmdSkill([]string{"install", "--dir", dir, "machin-start"})
	})
	if err != nil {
		t.Fatalf("cmdSkill install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "machin-start", "SKILL.md")); err != nil {
		t.Fatalf("expected machin-start installed: %v", err)
	}
}

func TestCmdSkillInstallUnknownErrors(t *testing.T) {
	dir := t.TempDir()
	err := withSilencedStdout(t, func() error {
		return cmdSkill([]string{"install", "--dir", dir, "unknown-skill"})
	})
	if err == nil {
		t.Fatal("expected error for install of unknown skill")
	}
}

func TestCmdSkillUnknownSubcommandErrors(t *testing.T) {
	err := withSilencedStdout(t, func() error { return cmdSkill([]string{"bogus"}) })
	if err == nil {
		t.Fatal("expected an error for an unknown subcommand")
	}
}

func TestEmbeddedSkillsHasAllOrderedNames(t *testing.T) {
	skills := embeddedSkills()
	for _, n := range skillOrder {
		if skills[n] == "" {
			t.Fatalf("embeddedSkills missing content for %q", n)
		}
	}
}
