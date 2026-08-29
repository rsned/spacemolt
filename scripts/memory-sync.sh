#!/usr/bin/env bash
# Mirror the Claude auto-memory directory into the repo so it is backed up and
# change-tracked by git. One-way: live memory -> docs/memory/. Never edit the
# repo copy; the next sync overwrites it.
#
# Usage: scripts/memory-sync.sh            (or: make memory-sync)
#        MEMORY_SRC=/path/to/memory scripts/memory-sync.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src="${MEMORY_SRC:-$HOME/.claude/projects/-home-robert-spacemolt-spacemolt/memory}"
dst="$repo_root/docs/memory"

if [[ ! -d "$src" ]] || [[ ! -f "$src/MEMORY.md" ]]; then
  echo "memory-sync: source '$src' is missing or has no MEMORY.md; refusing to sync" >&2
  exit 1
fi

mkdir -p "$dst"
# --delete keeps the mirror exact (retired memories disappear too).
# README.md is repo-only documentation and is preserved.
rsync -a --delete --exclude 'README.md' "$src/" "$dst/"

count=$(find "$dst" -name '*.md' ! -name README.md | wc -l)
echo "memory-sync: $count memory files mirrored to docs/memory/"
if git -C "$repo_root" status --short -- docs/memory | grep -q .; then
  git -C "$repo_root" status --short -- docs/memory
  echo "memory-sync: changes above are unstaged; commit with: git add docs/memory && git commit -m 'docs(memory): checkpoint'"
else
  echo "memory-sync: mirror already up to date"
fi
