#!/usr/bin/env bash
# Sync the Claude Code plugin's bundled skills from the canonical skills/ (which are
# also embedded in the binary). Run after editing any skills/<name>/SKILL.md.
set -e
cd "$(dirname "$0")/.."
for s in machin-start machin-web machin-backend machin-gamedev machin-deploy; do
  mkdir -p "plugins/machin/skills/$s"
  cp "skills/$s/SKILL.md" "plugins/machin/skills/$s/SKILL.md"
done
echo "synced 5 skills -> plugins/machin/skills/"
