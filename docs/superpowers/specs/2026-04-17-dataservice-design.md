# Dataservice Design

**Date:** 2026-04-17
**Status:** Approved

## Overview

A service where one agent (initially `databot`) acts as a query server for other agents. Other agents send private chat DMs; the service parses them as data queries against the shared knowledge base and replies in the same format the request used (plaintext → styled text, JSON → JSON).

The first handler is `nearest <poi_type>`. A pluggable handler registry supports future queries (crafting, etc.). The library is named `pkg/dataservice/` rather than `pkg/databot/` so that any agent identity can run it — a pirate, a scientist, a custom new role — without the package name implying a specific persona.

## Goals / Non-goals

### Goals

- Centralize read-only KB queries so every other agent does not need to embed duplicate query code.
- Symmetric format contract: plaintext request → styled plaintext reply; JSON request → JSON reply.
- Horizontally scalable: multiple service instances can run concurrently under different agent IDs. The binary accepts `--agent-id` and spawns one service per invocation.
- Pure idle in-game presence: the host agent logs in, stays docked, and does nothing but chat. Hands-free operation.
- Library separation: `pkg/dataservice/` is library-testable without spinning up a live game client; `cmd/databot/main.go` is trivial wiring.

### Non-goals (v1)

- LLM-based intent parsing. Strict grammar only.
- Multi-part replies for content exceeding the 500-char chat cap. v1 handlers must fit their output in one message.
- Public-channel listening or `@mention` prefixes. Private DMs only.
- Write operations or game actions initiated on behalf of other agents.

## Architecture

```
  other agents ──private DM──▶  databot game client (pure idle, docked)
                                    │
                                    ├── Ingest loop (every 5s)
                                    │     GetChatHistory → mbox.Ingest
                                    │
                                    ├── Dispatch loop
                                    │     mbox.List(unread, private, target_id==me)
                                    │     → Registry.Dispatch
                                    │     → Chat(reply, target_id=sender)  [1/tick]
                                    │     → mbox.MarkRead
                                    │
                                    └── reads shared SQLite KB (?mode=ro)
```

Two goroutines under a single `errgroup`. The shared KB is the same SQLite file other agents write to; SQLite WAL mode handles concurrent readers. The mbox is private to this agent and tracks read/unread state, which naturally solves the watermark problem.

## Components

### `pkg/dataservice/` (new package)

- `handler.go` — the `Handler` interface, shared `Request` / `Response` types, and a `Format` enum (plaintext / JSON).
- `registry.go` — `Registry` with `Register(Handler)`, `Dispatch(format Format, content string) (reply string, err error)`, and `Help(format Format) string` auto-generated from the set of registered handlers.
- `service.go` — the `Service` struct with `New(cfg Config) *Service` and `Run(ctx context.Context) error`. Owns ingest and dispatch loops.
- `handlers/nearest.go` — the first concrete handler.
- `reply.go` — `truncateReply(s)` 500-char safety helper, `detectFormat(content)` helper.
- `errors.go` — shared error shapes returned to the caller.

### Shared extraction — `pkg/galaxy/nearest_by_poi.go`

The existing `nearest` logic lives in `cmd/tools/play_as/nearest.go` and reaches for `globalKB` / `globalClock` / `globalGraphCache` globals. To make it callable from both `play_as` and the dataservice, extract the orchestration into a new function in `pkg/galaxy/` (or `pkg/knowledge/` if preferred after further exploration):

```go
func FindNearestByPOIType(
    ctx context.Context,
    kb knowledge.Base,
    graph *Graph,
    fromSystem string,
    poiType string,
    limit int,
) ([]NearestResult, error)
```

The existing `queryAccessibleStations` and `queryPOIsByType` helpers move into the new file. `cmd/tools/play_as/nearest.go` is refactored to call `FindNearestByPOIType` with its existing globals passed explicitly. The dataservice handler calls the same function with deps injected at construction time. No duplication.

### `cmd/databot/main.go` (new binary)

Flags:

```
--agent-id        string   (required)     agent identity to run as (e.g. "databot")
--db-path         string   (required)     shared KB path, opened ?mode=ro
--mbox-path       string   default: data/agents/<agent-id>/mbox.db
--server          string   default: from config
--poll-interval   duration default: 5s
--log-file        string   default: data/agents/<agent-id>/logs/dataservice.log
```

Startup flow:

1. Load credentials from `data/agents/<agent-id>/credentials.json`.
2. Connect to the game server and log in.
3. Open the shared KB with a read-only DSN (`?mode=ro`).
4. Open the per-agent mbox store.
5. Build the `Registry` and register handlers (`nearest` for v1).
6. Construct `Service` with the game client, registry, mbox, and KB deps, then call `Run(ctx)` until SIGINT/SIGTERM.

The binary itself is 80–120 lines of wiring with no unit tests — the testable logic lives in `pkg/dataservice/`.

### `data/agents/databot/personality.json`

Mirrors the shape of other agent personality files. Role `"DataService"`. `decision_mode: "none"` — no LLM loop; the dataservice goroutines drive behavior directly. Flavor: cheerful reference desk (accurate, helpful, lightly warm). Future databot instances spun up for load can have different flavors (cold archival AI, sarcastic research assistant, etc.) so agents asking the same question get contextually different prose while the data is identical.

## Data contracts

### Plaintext grammar

```
help
nearest <poi_type> from <system_id>
```

`from <system_id>` is required for `nearest` — the service cannot see the requester's game state, so the caller must state its location.

Example plaintext reply for `nearest station from sol-3`:

```
Nearest accessible station from sol-3 (Sol III):
  1. sol-2 (Sol II) — 1 hop, updated ~2h ago
  2. alpha-1 (Alpha Prime) — 2 hops, updated ~4h ago
  3. beta-2 (Beta II) — 3 hops, updated ~1d ago
```

The existing `formatNearestResultsStyled` output is condensed onto one line per result to stay comfortably under 500 chars.

Example plaintext error:

```
Error: missing "from <system_id>". Usage: nearest <poi_type> from <system_id>
Send 'help' for available commands.
```

### JSON schemas

Request:

```json
{"query": "nearest", "params": {"poi_type": "station", "from_system": "sol-3"}}
```

Success response:

```json
{
  "query": "nearest",
  "status": "ok",
  "from_system": "sol-3",
  "results": [
    {"system_id": "sol-2", "system_name": "Sol II", "hops": 1, "last_updated_tick": 12345}
  ]
}
```

Error response:

```json
{"query": "nearest", "status": "error", "error": "missing required field: from_system"}
```

Help response:

```json
{
  "query": "help",
  "status": "ok",
  "handlers": [
    {
      "name": "nearest",
      "description": "Find nearest accessible POIs of a given type",
      "plaintext_usage": "nearest <poi_type> from <system_id>",
      "json_example": {"query": "nearest", "params": {"poi_type": "station", "from_system": "sol-3"}}
    }
  ]
}
```

### Format detection

`strings.TrimSpace(content)` starts with `{` → JSON. Otherwise plaintext. If the content starts with `{` but fails to parse, the service replies with a JSON error (`{"status":"error","error":"invalid JSON: ..."}`).

## Loops and concurrency

Two goroutines under one `errgroup`:

**Ingest loop (every 5s):**

- Send `GetChatHistory(channel="private", {limit: 50})` and await the response.
- For each returned message, call `mbox.Ingest(msg)`. Deduplication against already-stored messages is free via `INSERT OR IGNORE` on the `id` primary key.

**Dispatch loop:**

- Query `mbox.List(Query{Channel: "private", UnreadOnly: true, Limit: 50})`.
- Filter: keep only messages where `target_id == myID` and `sender_id != myID`.
- Dedupe: within the pending batch, if two messages have identical `(sender_id, content)`, drop the older one (mark-read without replying).
- Process in FIFO order by `timestamp_utc`:
  - `reply, err := registry.Dispatch(format, content)`
  - `reply = truncateReply(reply)`
  - `client.Chat(channel="private", content=reply, target_id=msg.SenderID)`
  - Sleep `SleepTick` (10s) before the next send to respect the 1-mutation-per-tick server cap.
  - `mbox.MarkRead(msg.ID)`.

The 10s-per-reply cadence paces the dispatch loop naturally. The ingest loop keeps running during sends, so the queue stays current.

## Safety and guardrails

- **Self-reply prevention:** skip any message where `sender_id == myID`. Belt-and-suspenders since other agents shouldn't DM themselves with databot's ID, but cheap insurance.
- **500-char hard cap:** `truncateReply(s)` applied immediately before every `Chat()` call. If truncated, append `"…[truncated]"`.
- **Error taxonomy:**
  - Unknown query → reply names the unknown query and hints `send 'help' for commands`.
  - Parse error (missing/invalid field) → reply names the specific field.
  - Internal error (KB query fails, graph fails) → generic user-facing reply; full detail logged locally.
- **Read-only KB:** DSN opened with `?mode=ro`. SQLite enforces this at the driver level; no accidental write path exists.
- **Connection resilience:** the existing `pkg/game/Client` handles reconnects. The service loops log transient errors and retry on the next tick.

## Testing

- Unit tests per `Handler`: `ParsePlaintext` + `ParseJSON` + `Execute` + `Render*` for happy path and each error case in both formats.
- `Registry.Dispatch` tests: format detection, routing, unknown-query response shape, help generation covers all registered handlers.
- Integration test: fake `GameClient` feeds synthetic `ChatMessage` events into the service; assert the outgoing `Chat()` calls (content, channel, `target_id`).
- Mbox dedupe logic exercised via a seeded mbox.
- `cmd/databot/main.go` is not unit-tested — trivial wiring verified by `go build ./...` and manual smoke test.

## Deferred / future work

- Crafting handler: `can_craft` (what recipes are craftable from a given cargo array).
- Public-channel listening via `@databot` mention prefix.
- LLM intent parsing for natural-language queries.
- Multi-part replies for long content (numbered chunks).
- Per-sender rate limiting if abuse appears (mbox makes abuse auditable post-hoc).
- Metrics surface via `pkg/monitor` (query counts, latencies, error rates).
- `status` handler reporting uptime and total queries served.
- Additional databot instances with distinct personalities to distribute load.

## Open questions (non-blocking)

- Which system should databot be docked in for v1? Default to manually docking once via `play_as`; game state persists the docked location across restarts.
