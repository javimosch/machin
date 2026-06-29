package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The Claude Code plugin (plugins/machin/skills/) bundles copies of the canonical
// skills/ (which are also embedded in the binary). Keep them byte-identical so the
// plugin never ships stale guidance — run tools/sync-plugin-skills.sh after edits.
func TestPluginSkillsInSync(t *testing.T) {
	skills := []string{"machin-start", "machin-web", "machin-backend", "machin-gamedev", "machin-deploy"}
	for _, s := range skills {
		canon, err := os.ReadFile(filepath.Join("skills", s, "SKILL.md"))
		if err != nil {
			t.Fatalf("canonical skill %s: %v", s, err)
		}
		bundled, err := os.ReadFile(filepath.Join("plugins", "machin", "skills", s, "SKILL.md"))
		if err != nil {
			t.Fatalf("plugin skill %s missing — run tools/sync-plugin-skills.sh: %v", s, err)
		}
		if !bytes.Equal(canon, bundled) {
			t.Errorf("plugin skill %s is out of sync with skills/%s — run tools/sync-plugin-skills.sh", s, s)
		}
	}
}
