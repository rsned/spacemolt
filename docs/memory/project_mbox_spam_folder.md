---
name: project_mbox_spam_folder
description: mbox spam folder + per-agent blocklist for muting noisy chat senders
metadata: 
  node_type: memory
  type: project
  originSessionId: a9e877bc-b5aa-417f-b398-a7b8f6f7c432
---

Built 2026-05-31. Lets `play_as` users mute noisy senders (local/system spam,
NPC customs, market shouts). Blocked senders' chat is still captured — flagged
into a **spam folder** — but never printed to the console.

- **Schema migration 4** (`pkg/mbox/store.go`): `spam_at TEXT` column + partial
  index. A message is "in spam" when `spam_at IS NOT NULL` (mirrors the
  `deleted_at` soft-delete pattern). Excluded from `List`/`Search`/`UnreadCounts`
  by default; `Query.SpamOnly` selects the folder, `Query.IncludeSpam` includes it.
- **Blocklist** (`pkg/mbox/blocklist.go`): JSON array of strings at
  `data/agents/<agent>/spam_list.json`. Matches sender_id OR display name,
  case-insensitive. `LoadBlocklist`/`Add`/`Remove`/`IsBlocked`(nil-safe)/`List`.
- **Auto-spam on ingest**: `Ingester.SetBlocklist(bl)` → `ingestAPI` stamps
  `SpamAt` for blocked senders on every path (push/poll/reconcile).
- **Console suppression**: both display paths in `cmd/tools/play_as/main.go` skip
  blocked senders — the WS push callback (~line 318) and `chatPoller.displayMessage`.
- **Commands**: `mark_spam <user>` / `unmark_spam <user>` / `spam_list`
  (top-level aliases), plus `mbox mark-spam|unmark-spam|spam-list` and
  `mbox list spam`. mark_spam retroactively flags the sender's stored messages.
- Design doc: `docs/plans/2026-05-31-mbox-spam-folder-design.md`. Relates to
  [[reference_server_docs_sync]].
