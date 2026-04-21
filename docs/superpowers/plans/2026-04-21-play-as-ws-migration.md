# play_as WS Migration Plan

**Started:** 2026-04-21
**Status:** Phase 1 shipped; Phases 2-6 pending.

## Context

`cmd/tools/play_as/` originally used MCP-over-HTTP because WebSocket
connections were unstable (60-90s reconnect cycles). Earlier today the
WS client was stabilized (active ping loop at 20s, force-close on 2
consecutive ping failures, Disconnect mutex deadlock fix). With WS now
steady, the REPL can move to the transport that actually receives
server-initiated push events (combat warnings, chat, ticks, etc.) that
MCP's request/response model cannot deliver in time for agents to react.

The architecture was already transport-agnostic: both `*game.Client` (WS)
and `*game.MCPGameClient` implement the `GameClient` interface and
`XPCallbackSetter`. The migration is narrow — not a rewrite.

## Phase 1 — Transport switch ✅ (shipped 2026-04-21)

- Added `--transport=ws|mcp` flag, default `ws`.
- `cmd/tools/play_as/main.go:82` now dispatches to either
  `game.InitializeAgent` (WS) or `game.InitializeMCPAgent` (MCP).
- printUsage lists the flag.
- Existing tests pass unchanged.

**Validation:** user is running it live to confirm WS stability
manifests correctly in the REPL.

## Phase 2 — Push ingestion correctness (pending)

The chat poller at `main.go:256` was an MCP workaround. On WS,
`SetOnChatMessage` already feeds mbox via push, so the poller
duplicates work and can double-write.

**Decision:** gate the poller to MCP-only, OR on WS switch it to
reconciler mode at reduced frequency (~`SleepChatPoll * 5`) to catch
messages missed during the ~15s reconnect window.

**Recommendation:** reconciler mode. Keeps safety net without
primary-source duplication.

**Files:**
- `cmd/tools/play_as/main.go` — chatPoller init block, ~line 253-259.
- Possibly `cmd/tools/play_as/chat_poller.go` (if poll interval is
  baked in there) — pass interval as option.

## Phase 3 — Server-event surfacing (pending)

WS delivers events MCP never did: `combat_update`, `pirate_warning`,
`police_warning`, `mining_yield`, `skill_level_up`. Currently these
update internal state silently via `client.handleResponse()` but the
REPL renders nothing.

**Decisions to make:**
- Which events interrupt the prompt line?
  - Time-critical (must show): `pirate_warning`, `police_warning`,
    `combat_update`.
  - Info-only (should show, probably not interrupt): `skill_level_up`,
    `mining_yield`.
  - Already handled: chat (via mbox), tick (via statusline).

**Files:**
- `pkg/game/client.go` — add `SetOn<Event>` callbacks if they don't
  already exist; or a single `SetOnServerEvent(func(type, payload))`
  fan-out. (Check: `SetOnChatMessage` and `SetOnStorageUpdate` already
  exist, follow that pattern.)
- `cmd/tools/play_as/main.go` — wire the callbacks in runREPL.
- New `cmd/tools/play_as/events.go` for the render helpers.

**Minimum bar for first cut:** combat + pirate + police warnings
printed inline above the prompt with `\r` to not corrupt liner input.
Others can ship in follow-up.

## Phase 4 — Reconnect UX in REPL (pending)

WS disconnects now recover in ~15s instead of ~70s, but during that
window commands fail silently (or with cryptic errors). User doesn't
see why.

**Tasks:**
- Add a reconnecting indicator to `cmd/tools/play_as/statusline.go`.
  Check `client.IsConnected()` on render and show a dot/spinner when
  false.
- When a command returns an error and `!client.IsConnected()`, print
  `⟳ reconnecting, retry in a moment` rather than the raw error.

Don't bother queuing commands and auto-retrying — user can re-submit
after the indicator clears.

**Files:**
- `cmd/tools/play_as/statusline.go`
- `cmd/tools/play_as/main.go` — command-dispatch error path.

## Phase 5 — Lifecycle cleanup (pending)

`main.go` currently relies on process exit to tear down the client.
With WS's goroutines (listen, ping, health, command queue), a clean
`Disconnect()` on exit is polite and avoids rare spurious server-side
disconnect telemetry.

**Task:** add `defer client.Disconnect()` (or equivalent method on
`GameClient`) after init.

**Check:** does `GameClient.Close()` already call `Disconnect()`? If
yes, the existing deferred `client.Close()` may be sufficient. Verify
before assuming.

## Phase 6 — Tests (pending)

- Existing unit tests (`explore_test.go`, `missions_test.go`,
  `nearest_test.go`, `loop_block_test.go`, `example_missions_test.go`)
  all use mocks via `GameClient`. They should pass unchanged.
- Add a transport-flag parse test: `--transport=ws` and
  `--transport=mcp` pick the right initializer path. Keep it at the
  flag-wiring level; no network.

## Out of scope (deliberately)

- Removing MCP client code from `pkg/game/`. Still used by
  `cmd/bridge/mcp-*` and its own tests. Keep as-is.
- Changing `InitializeAgent` signature.
- Changing any other `cmd/` tool — this migration is `play_as` only.

## Sequence

1. Phase 1 ✅
2. Phase 2 + 4 together (they overlap in REPL glue)
3. Phase 3
4. Phase 5 + 6 ride along with whichever of the above is going out
