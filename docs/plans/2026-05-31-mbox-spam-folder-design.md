# mbox Spam Folder & Blocklist — Design

Date: 2026-05-31

## Goal

Let a `play_as` user silence noisy chat senders (local/system spam, NPC
customs spam, market shouts). Blocked senders' messages are still captured —
they land in a new **spam folder** in the mbox — but they are not printed to
the console.

## Requirements (confirmed)

- A **spam folder** in mbox.
- A **`mark_spam <user>`** command to block a sender.
- The blocked-user list is persisted to `data/agents/<agent>/spam_list.json`
  as a JSON array of strings.
- When a `chat_message` arrives from a blocked user it is stored in the spam
  folder and **not displayed** to the console.
- Matching is by **sender_id OR display name**, case-insensitive.
- `mark_spam` **retroactively** flags that sender's already-stored messages.
- Full management command set: `mark_spam`, `unmark_spam`, `spam_list`,
  and `mbox list spam`.

## Data model & storage

Spam is a **flag**, not a server channel (the server owns `channel`). Mirror
the existing soft-delete (`deleted_at`) pattern.

- **Migration 4** (`pkg/mbox/store.go`):
  `ALTER TABLE messages ADD COLUMN spam_at TEXT;` plus a partial index for
  spam listing. A message is "in spam" when `spam_at IS NOT NULL`; its original
  channel is preserved.
- **`Message`** gains `SpamAt *time.Time`.
- Normal listings (`mbox list`, `UnreadCounts`) **exclude** spam, exactly as
  they exclude soft-deleted rows. `mbox list spam` selects `spam_at IS NOT NULL`.
- New store methods:
  - `MarkSpamBySender(idOrName string) (int, error)`
  - `UnmarkSpamBySender(idOrName string) (int, error)`
  - `ListSpam(opts ListOptions) ([]Message, error)`
  - `Ingest` persists `SpamAt` when the caller sets it.

### Blocklist (`pkg/mbox/blocklist.go`, new)

Backed by `data/agents/<agent>/spam_list.json` (JSON array of strings).

```go
type Blocklist struct { path string; mu sync.RWMutex; entries map[string]bool }
func LoadBlocklist(path string) (*Blocklist, error)         // missing file -> empty, no error
func (b *Blocklist) IsBlocked(senderID, sender string) bool // case-insensitive, matches EITHER
func (b *Blocklist) Add(entry string) (bool, error)         // add + atomic JSON write
func (b *Blocklist) Remove(entry string) (bool, error)
func (b *Blocklist) List() []string
```

Entries are stored verbatim and matched case-insensitively against both
`sender_id` and `sender`.

## Wiring & commands

### Auto-spam on ingest

`Ingester` gets `SetBlocklist(bl)`. In `ingestAPI`, after building the
`Message`, if `bl.IsBlocked(senderID, sender)` it stamps `m.SpamAt = &now`
before `store.Ingest`. Covers push, poll, and reconcile paths uniformly.

### Console suppression (two paths in cmd/tools/play_as/main.go)

- Push callback (~line 318): still `HandlePush` (stores as spam), but `return`
  before `displayMessage` when blocked.
- Poller: add `blocklist *mbox.Blocklist` to `chatPoller`; `displayMessage`
  returns early when blocked. Ingest already happens above the display loop in
  `poll()`, so the message is still captured.

Both share the one `Blocklist` loaded next to the mbox open (~line 291).

### Commands

Existing mbox subcommands are hyphenated; add them there plus top-level aliases
matching the requested name.

| Command | Action |
|---|---|
| `mark_spam <user>` (alias `mbox mark-spam`) | `bl.Add` -> `MarkSpamBySender`; prints `blocked <user>; moved N message(s) to spam` |
| `unmark_spam <user>` (alias `mbox unmark-spam`) | `bl.Remove` -> `UnmarkSpamBySender`; prints restored count |
| `spam_list` (alias `mbox spam-list`) | prints blocked entries |
| `mbox list spam` | lists messages in the spam folder |

## Testing (TDD)

- `blocklist_test.go`: missing file -> empty; Add/Remove persist + JSON
  round-trip; `IsBlocked` matches by id and by name, case-insensitively;
  duplicate Add is a no-op.
- `store_test.go`: `MarkSpamBySender` flags existing rows and excludes them
  from `List`/`UnreadCounts`; `ListSpam` returns them; `UnmarkSpamBySender`
  reverses; `Ingest` with `SpamAt` set persists; migration-4 round-trip on a
  fresh DB.
- Ingester test: a blocked sender's pushed message is stored with
  `SpamAt != nil`.
