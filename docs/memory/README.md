# Claude auto-memory mirror

This directory is a **snapshot mirror** of Claude Code's persistent memory for
this project. The live copy lives outside the repo at
`~/.claude/projects/-home-robert-spacemolt-spacemolt/memory/`; that is the
directory Claude reads and writes during sessions.

- **Do not edit files here.** The next sync overwrites the whole directory
  (`rsync --delete`), so edits belong in the live copy.
- **To checkpoint:** `make memory-sync`, then
  `git add docs/memory && git commit -m 'docs(memory): checkpoint'`.
- **Changelog:** `git log --stat -- docs/memory` shows which memories changed
  at each checkpoint; `git diff <rev> -- docs/memory/<file>.md` shows how.

`MEMORY.md` is the index Claude loads each session; every other file is one
memory (frontmatter `name`/`description`/`type`, then the fact).
