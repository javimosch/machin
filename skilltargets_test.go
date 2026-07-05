package main

import (
	"os"
	"path/filepath"
	"testing"
)

// skillTargets and skillInstall had no direct test coverage; cover the target
// resolution rules (explicit --dir wins, env var, ~/.agents/skills, and the
// conditional ~/.claude/skills) and the actual file-writing side of install.

func TestSkillTargetsExplicitDirWins(t *testing.T) {
	t.Setenv("AGENT_SKILLS_DIR", "/should/be/ignored")
	got := skillTargets("/explicit/dir")
	if len(got) != 1 || got[0] != "/explicit/dir" {
		t.Fatalf("got %v, want [/explicit/dir]", got)
	}
}

func TestSkillTargetsDetectsClaudeDirWhenPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENT_SKILLS_DIR", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := skillTargets("")
	want := []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".claude", "skills"),
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSkillTargetsOmitsClaudeDirWhenAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENT_SKILLS_DIR", "")

	got := skillTargets("")
	want := []string{filepath.Join(home, ".agents", "skills")}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSkillTargetsIncludesAgentSkillsDirEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENT_SKILLS_DIR", "/custom/agent/dir")

	got := skillTargets("")
	if len(got) == 0 || got[0] != "/custom/agent/dir" {
		t.Fatalf("expected AGENT_SKILLS_DIR first, got %v", got)
	}
}

func TestSkillInstallWritesAllSkillsToExplicitDir(t *testing.T) {
	dir := t.TempDir()
	if err := skillInstall([]string{"--dir", dir}); err != nil {
		t.Fatalf("skillInstall: %v", err)
	}
	for _, n := range skillOrder {
		p := filepath.Join(dir, n, "SKILL.md")
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("expected %s to be written: %v", p, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s is empty", p)
		}
	}
}

func TestSkillInstallSingleNamedSkill(t *testing.T) {
	dir := t.TempDir()
	if err := skillInstall([]string{"--dir", dir, "machin-web"}); err != nil {
		t.Fatalf("skillInstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "machin-web", "SKILL.md")); err != nil {
		t.Fatalf("expected machin-web installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "machin-deploy")); err == nil {
		t.Fatal("machin-deploy should not have been installed")
	}
}

func TestSkillInstallUnknownSkillErrors(t *testing.T) {
	dir := t.TempDir()
	if err := skillInstall([]string{"--dir", dir, "not-a-real-skill"}); err == nil {
		t.Fatal("expected an error for an unknown skill name")
	}
}

func TestSkillInstallNoTargetErrors(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("AGENT_SKILLS_DIR", "")
	if err := skillInstall(nil); err == nil {
		t.Fatal("expected an error when no install target can be resolved")
	}
}
