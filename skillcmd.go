package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// `machin skill` registers the embedded SKILL.md files where coding agents actually
// look — closing the discovery loop. The neutral ~/.agents/skills is the canonical
// copy; detected runtimes (Claude Code reads ~/.claude/skills/<name>/SKILL.md, the
// same format these already use) get them too, so machin surfaces at the decision
// moment in a real session, not only inside this repo.

// skillOrder is the install/list order; machin-start (the decision skill) is first.
var skillOrder = []string{"machin-start", "machin-web", "machin-gamedev", "machin-backend", "machin-deploy"}

// embeddedSkillsMap is built once; embeddedSkills is a thin, allocation-free
// accessor so callers (resolveSkill, cmdSkill, skillList, skillInstall) don't
// each rebuild the map on every call.
var embeddedSkillsMap = map[string]string{
	"machin-start":   skillStart,
	"machin-web":     skillWeb,
	"machin-gamedev": skillGamedev,
	"machin-backend": skillBackend,
	"machin-deploy":  skillDeploy,
}

func embeddedSkills() map[string]string {
	return embeddedSkillsMap
}

// skillAliases maps the short `--skill` names (and "machin") to the full skill dir name.
var skillAliases = map[string]string{
	"start": "machin-start", "machin": "machin-start",
	"web": "machin-web", "gamedev": "machin-gamedev", "game": "machin-gamedev",
	"backend": "machin-backend", "deploy": "machin-deploy",
}

func resolveSkill(name string) string {
	skills := embeddedSkills()
	if _, ok := skills[name]; ok {
		return name
	}
	if full, ok := skillAliases[name]; ok {
		return full
	}
	return ""
}

// skillTargets is where `machin skill install` writes by default: an explicit --dir,
// else the vendor-neutral ~/.agents/skills plus any agent runtime detected on disk.
func skillTargets(explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(os.Getenv("AGENT_SKILLS_DIR"))
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		add(filepath.Join(home, ".agents", "skills")) // vendor-neutral canonical
		// Claude Code reads ~/.claude/skills — only target it if it's installed,
		// so we don't litter ~/.claude on machines without it.
		if fi, err := os.Stat(filepath.Join(home, ".claude")); err == nil && fi.IsDir() {
			add(filepath.Join(home, ".claude", "skills"))
		}
	}
	return out
}

func cmdSkill(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "list", "-h", "--help":
		return skillList()
	case "show":
		if len(args) < 1 {
			return fmt.Errorf("usage: machin skill show <name>")
		}
		n := resolveSkill(args[0])
		if n == "" {
			return fmt.Errorf("unknown skill %q (try: %v)", args[0], skillOrder)
		}
		fmt.Print(embeddedSkills()[n])
		return nil
	case "install":
		return skillInstall(args)
	default:
		return fmt.Errorf("unknown skill subcommand %q (use: list, show, install)", sub)
	}
}

func skillList() error {
	fmt.Println("embedded skills:")
	for _, n := range skillOrder {
		fmt.Printf("  %s\n", n)
	}
	fmt.Println("\ndefault install targets (where agents look):")
	t := skillTargets("")
	if len(t) == 0 {
		fmt.Println("  (none detected — pass --dir or set AGENT_SKILLS_DIR)")
	}
	for _, d := range t {
		fmt.Printf("  %s\n", d)
	}
	fmt.Println("\n  machin skill install                 # all skills -> the targets above")
	fmt.Println("  machin skill install machin-start    # just one")
	fmt.Println("  machin skill install --dir ./.claude/skills   # an explicit dir (e.g. a project)")
	fmt.Println("  machin skill show machin-start       # print one to stdout")
	return nil
}

func skillInstall(args []string) error {
	dir := ""
	var names []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir":
			if i+1 < len(args) {
				dir = args[i+1]
				i++
			}
		case "--all":
			// default behavior; accepted for clarity
		default:
			n := resolveSkill(args[i])
			if n == "" {
				return fmt.Errorf("unknown skill %q (try: %v)", args[i], skillOrder)
			}
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		names = skillOrder
	}
	targets := skillTargets(dir)
	if len(targets) == 0 {
		return fmt.Errorf("no install target — pass --dir <path> or set AGENT_SKILLS_DIR")
	}
	skills := embeddedSkills()
	for _, t := range targets {
		for _, n := range names {
			p := filepath.Join(t, n)
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(skills[n]), 0o644); err != nil {
				return err
			}
		}
		fmt.Printf("machin: installed %d skill(s) -> %s\n", len(names), t)
	}
	return nil
}
