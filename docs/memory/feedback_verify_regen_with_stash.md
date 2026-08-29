---
name: feedback_verify_regen_with_stash
description: "When proving a shared generator's output is unchanged, swap the old code in with git stash — never `git checkout <sha> -- file`, which silently destroys uncommitted work in that file"
metadata: 
  node_type: memory
  type: feedback
  originSessionId: e7789b50-07fd-46fe-856d-85f76b11e40d
  modified: 2026-07-27T01:20:07.183Z
---

To prove a change to a shared renderer/generator leaves existing callers'
output identical, the check is: generate with the NEW code, swap in the OLD
code, generate again, compare the two outputs.

**Do the swap with `git stash push <file>` / `git stash pop`.** I used
`git checkout <sha> -- pkg/galaxymap/galaxymap.go` for the swap and then
`git checkout HEAD -- <file>` to "restore" — which restored the *committed*
version and silently deleted a substantial set of uncommitted edits I had just
written to that file. Had to redo them.

**Why:** `git checkout -- <file>` overwrites the working tree with no warning
and no reflog entry for working-tree content. `git stash` round-trips the
uncommitted state safely.

**Also:** generating twice with the NEW code and diffing the results only proves
determinism, not equivalence with the old behavior. That was written into a plan
as a fallback and is not a valid substitute — always compare old-vs-new against
the same inputs. Both traps hit in the same session while shipping
[[project_stronghold_reach_page]].
