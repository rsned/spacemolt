# Agent Mbox — Local Message Store Design

**Date:** 2026-04-15
**Status:** Approved

## Problem

Chat messages (system, local, faction, private) arrive via server push and `get_chat_history` polling, but nothing persists them locally. The existing `chatPoller` in `play_as` prints to stdout and forgets. Agents have no durable memory of prior conversations, and the human operator has no organized way to browse what an agent received.

## Goals

1. Per-agent SQLite mbox at `data/agents/<name>/mbox.db` storing all chat messages.
2. Push-first ingestion from server `chat_message` events, plus a bounded backfill on login.
3. Single shared `read_at` state for marking messages processed by human or agent.
4. FTS5 full-text search over content and sender.
5. CLI commands in `play_as` REPL for browsing, searching, and marking read.
6. Agent-side helpers for structured queries ("what did sender X say?", "any unread matching keywords?").

## Non-goals (follow-up work)

- Web UI (React frontend "Messages" tab) — separate spec.
- Threading / reply-to tracking.
- Agent-side "reply to this message" automation.
- Private DM backfill (on-demand only — requires `target_id` per conversation).
- Cross-agent message aggregation.
- ToT prompt integration for "Inbox Highlights" context.

## Architecture

### Package layout

New package: `pkg/mbox`

Three units:

| File | Type | Purpose |
|------|------|---------|
| `store.go` | `Store` | SQLite connection, CRUD, queries, FTS5 search, migrations |
| `ingest.go` | `Ingester` | Push handler, backfill crawler, optional reconciler |
| `cli.go` | helpers | Terminal formatting for `play_as` REPL commands |

### Wiring

`pkg/game/Client` gains an optional callback:

```go
OnChatMessage func(serverapi.ChatMessage)
```

Set to nil by default. `handleResponse` calls it from the existing `chat_message` case when non-nil. This avoids changing the `MessageHandler` interface.

Agent startup code and `play_as` construct `Store` + `Ingester`, install the callback, and call `Backfill()` once after `logged_in`.

### What does NOT change

- `pkg/knowledge` schema and tables.
- Agent `Runner` decision loop.
- Protocol layer.
- `State.LastChatHistory` (still used by compound actions).

## Schema

`data/agents/<name>/mbox.db` — SQLite with WAL mode.

```sql
CREATE TABLE messages (
    id            TEXT PRIMARY KEY,
    channel       TEXT NOT NULL,             -- system|local|faction|private
    sender_id     TEXT NOT NULL,
    sender        TEXT NOT NULL,
    content       TEXT NOT NULL,
    target_id     TEXT,
    target_name   TEXT,
    timestamp_utc TEXT NOT NULL,             -- RFC3339 from server
    ingested_at   TEXT NOT NULL,             -- RFC3339 local insert time
    read_at       TEXT,                      -- NULL = unread
    source        TEXT NOT NULL              -- 'push' | 'backfill' | 'reconcile'
);

CREATE INDEX idx_messages_channel_ts ON messages(channel, timestamp_utc DESC);
CREATE INDEX idx_messages_unread     ON messages(channel, read_at) WHERE read_at IS NULL;
CREATE INDEX idx_messages_sender     ON messages(sender_id, timestamp_utc DESC);

CREATE TABLE channel_cursors (
    channel          TEXT PRIMARY KEY,
    oldest_seen_utc  TEXT NOT NULL,
    newest_seen_utc  TEXT NOT NULL,
    last_backfill_at TEXT NOT NULL,
    backfill_capped  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE schema_version (version INTEGER PRIMARY KEY);

CREATE VIRTUAL TABLE messages_fts USING fts5(
    content, sender, content='messages', content_rowid='rowid',
    tokenize='porter unicode61'
);

CREATE TRIGGER messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content, sender)
    VALUES (new.rowid, new.content, new.sender);
END;

CREATE TRIGGER messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content, sender)
    VALUES ('delete', old.rowid, old.content, old.sender);
END;
```

### Migration strategy

`Store.migrate()` reads `schema_version`, applies numbered steps, writes new version. Same pattern as `pkg/knowledge/sqlite_migrations.go` but local to mbox — no shared migration code.

## Store API

```go
type Store struct { ... }

func Open(dbPath string) (*Store, error)
func (s *Store) Close() error

// Write
func (s *Store) Ingest(msg Message, source string) (inserted bool, err error)
func (s *Store) MarkRead(ids ...string) error
func (s *Store) MarkChannelRead(channel string) error

// Read
func (s *Store) List(q Query) ([]Message, error)
func (s *Store) Search(text string, q Query) ([]Message, error)
func (s *Store) UnreadCounts() (map[string]int, error)
func (s *Store) Get(id string) (*Message, error)

// Cursors
func (s *Store) Cursor(channel string) (oldestSeen time.Time, exists bool, err error)
func (s *Store) SetCursor(channel string, oldest time.Time, capped bool) error

// Diagnostics
func (s *Store) SourceCounts() (map[string]int, error)
```

```go
type Query struct {
    Channel    string
    SenderID   string
    UnreadOnly bool
    Before     time.Time
    After      time.Time
    Limit      int    // default 20
    Offset     int
}
```

```go
type Message struct {
    ID           string
    Channel      string
    SenderID     string
    Sender       string
    Content      string
    TargetID     string
    TargetName   string
    TimestampUTC time.Time
    IngestedAt   time.Time
    ReadAt       *time.Time
    Source       string
}
```

## Ingester

```go
type Ingester struct {
    store *Store
}

func NewIngester(store *Store) *Ingester
```

### Push handler

```go
func (ing *Ingester) HandlePush(msg serverapi.ChatMessage)
```

Called from the `OnChatMessage` callback installed on `Client`. Parses the server message and calls `Store.Ingest(msg, "push")`.

### Backfill

```go
type BackfillOptions struct {
    MaxPerChannel   int           // default 500
    RequestInterval time.Duration // default SleepQuick (2s)
    Channels        []string      // default: system, local, faction
}

type BackfillReport struct {
    Channels map[string]ChannelReport
}

type ChannelReport struct {
    Fetched int
    Capped  bool
}

func (ing *Ingester) Backfill(ctx context.Context, client game.GameClient, opts BackfillOptions) (BackfillReport, error)
```

**Algorithm:**

1. For each channel, walk `get_chat_history` newest-first using `before` cursor.
2. For each message: call `Store.Ingest`. If `inserted=false`, stop — we've reached stored history.
3. If `fetched >= MaxPerChannel`, stop and set `backfill_capped=1` on the cursor.
4. Sleep `RequestInterval` between API calls.
5. Read raw response via `client.GetRawJSON("_last")` (same pattern as existing `chatPoller.fetchMessages`).
6. On error mid-crawl: persist what we have, return partial report.

### Reconciler (optional, default off)

Background goroutine polling last 20 msgs per channel every 10 minutes. Calls `Store.Ingest(msg, "reconcile")`. Provides empirical data on push completeness via `SourceCounts()`. Intended to be removed once push is confirmed reliable.

```go
func (ing *Ingester) StartReconciler(ctx context.Context, client game.GameClient, interval time.Duration)
```

## CLI surface

Added to `cmd/tools/play_as/main.go` REPL as `mbox` command.

```
mbox                                    show unread counts per channel
mbox list [channel] [--unread] [-n N]   list messages, newest first (default 20)
mbox show <id>                          show full message detail
mbox search "<query>" [--channel X]     FTS5 search over content + sender
mbox read <id>|--all|--channel X        mark read (single / all / per-channel)
mbox backfill [--channel X] [--limit N] deep crawl (default cap 500)
mbox sources                            diagnostic: push/backfill/reconcile counts
```

**Display formatting:**

- Channel name in color (system=cyan, local=yellow, faction=magenta, private=green) — same colors as existing `chatPoller`.
- Relative timestamps ("2m ago", "1h ago", "3d ago").
- Unread messages marked with a bullet or bold sender.
- `mbox` (no args) shows unread counts plus a hint about capped channels.

## Agent-side helpers

Added to `BaseAgent` via an optional `*mbox.Store` field:

```go
func (a *BaseAgent) RecentFromSender(senderID string, n int) []mbox.Message
func (a *BaseAgent) UnreadMatching(keywords []string) []mbox.Message
func (a *BaseAgent) MarkProcessed(ids ...string)
```

- `RecentFromSender` wraps `Store.List(Query{SenderID: senderID, Limit: n})`.
- `UnreadMatching` wraps `Store.Search` with `UnreadOnly=true`.
- `MarkProcessed` wraps `Store.MarkRead`.

These are convenience wrappers — agents can also use `Store` directly for more complex queries.

## Testing

- **`pkg/mbox/store_test.go`** — table-driven: ingest dedupe, list by channel/sender/unread, FTS5 search, cursor round-trip, mark-read, migrations. Uses `:memory:` SQLite. No mocks.
- **`pkg/mbox/ingest_test.go`** — backfill with a fake `GameClient` returning canned `ChatHistoryResponse` pages. Verify: early exit on known ID, cap enforcement, partial-failure cursor persistence, concurrent push during backfill, source tagging.
- **CLI** — smoke-test command parsing only; business logic tested via store/ingest tests.

## Error handling

- SQLite busy/locked: WAL mode handles concurrent reads. Write contention between push handler goroutine and backfill goroutine handled by SQLite's built-in busy timeout (set to 5s).
- `GetChatHistory` failures: log, skip, resume next channel or next session.
- Corrupt mbox.db: `Store.Open` runs `PRAGMA integrity_check` on first open; if corrupt, rename to `.db.bak` and start fresh with logged warning.
