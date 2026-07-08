# API Struct / Command Drift Audit — Design

**Date:** 2026-07-08
**Status:** Design approved, ready for implementation plan
**Task:** `project_api_struct_drift_audit` (memory)

## Problem

`BuiltForAPIVersion` in `pkg/version/checker.go` was bumped `v0.397.0` → `v0.473.0`
(commit `af8e099`) to match the catalog snapshot version, **without** an accompanying
audit of response structs or command signatures. That constant is documented to mean
"response structs + command signatures verified against this server version." The client
therefore now *asserts* compatibility across ~75 minor versions (0.398 → 0.473) that
nobody checked.

An unaudited version claim is worse than an honest stale one: `CheckVersion` will stay
silent, so genuine runtime incompatibilities won't surface as drift warnings.

**Goal:** audit the client's API surface against the authoritative v0.473.0 snapshot,
**fix the clear/mechanical breaks**, document the ambiguous ones, add a durable guardrail,
and end with an honest statement of what the version claim actually covers.

## Definition of Done

1. Three-layer drift diff produced (command coverage, payload shape, response fields).
2. Clear/mechanical breaks fixed in code (missing commands, retired/renamed commands,
   unambiguous field renames). `go build ./...` **and** `go test ./...` green.
3. Ambiguous/risky findings documented in the report, not touched.
4. Reverse-direction guardrail test added (server command → client coverage).
5. Findings report written with an explicit, honest verdict on the `BuiltForAPIVersion`
   claim (fully verified / partially verified with named gaps).

## Authoritative Sources (all ~v0.473, dated 2026-07-06/07)

| Source | Content | Role |
|--------|---------|------|
| `data/game-api/latest/get_commands.json` | 214 commands: `name`, `format` (payload shape), `is_mutation`, `category` | Layer 1 + 2 authority |
| `server_docs/openapi.json` (→ `openapi.20260706.json`) | 256 component schemas incl. a `*Response` per command; 207 paths | Layer 3 broad authority |
| `data/game-api/latest/*.json` (~28 files) | Live response samples (get_status, get_ship, view_market, …) | Layer 3 ground-truth spot-check |

## Client Surface Audited

- `pkg/game/client_commands.go` (+ any other `pkg/game/client*.go`) — command `Type` strings and Payload maps
- `pkg/game/serverapi/responses.go` — response structs (~2690 lines)
- `pkg/actionspace/actions.go` — `AllActions`
- `pkg/game/interface.go` — `GameClient` methods (coverage + mock impact)

## Approach: script-driven diff → triage → fix

Both sides are structured JSON / Go source, so a throwaway script does the tedious
cross-referencing and emits candidate-drift lists; human judgment is spent only on
verdicts. Chosen over manual read-through (error-prone at 214×256) and over a multi-agent
workflow (tractable inline; no workflow opt-in given).

### The diff script — `scratchpad/drift_audit.py`

Throwaway, lives in scratchpad (not committed). Emits three candidate lists.

**Layer 1 — Command coverage.**
Server 214 commands vs (client `Type` strings ∪ `AllActions`). Note the naive
`Type: "..."` regex has false positives — commands whose `Type` comes from a const,
variable, or helper (e.g. `craft`, `get_location`, `get_notifications` are known to exist
but didn't match the literal-string scan). The script lists candidates; triage confirms.
Buckets:
- **server-cmd-missing-from-client** — real gap → add method (if the client should support it) or add to the guardrail ignore-list (if intentionally unsupported, e.g. `v2_*`, `subscribe_*` streaming).
- **client-Type-not-on-server** — retired/renamed on the server (the `salvage_wreck` → `scrap_wreck`+`sell_wreck` pattern) → remove/rename client-side.
- **matched** — carry into Layer 2.

**Layer 2 — Payload shape.**
For matched commands, parse the JSON payload in the server `format` field, collect its
keys, and diff against the keys the client puts in its Payload map. Flag missing / extra /
renamed keys. (Best-effort: some client payloads are built dynamically; those get a
"manual-check" flag rather than a hard finding.)

**Layer 3 — Response field drift.**
For each struct in `responses.go`, resolve its authority in priority order:
1. Live sample JSON in `data/game-api/latest/` (ground truth) — if present.
2. Matching `*Response` schema in `openapi.json`.
3. Neither → **unverified-no-source** (listed, not guessed).

Diff the struct's json tags vs the schema/sample properties. Buckets: match, drift
(renamed/removed/added field), unverified.

### Triage + fix

Each candidate finding gets a verdict:
- **Real drift, mechanical** → fix now. Missing commands, retired-command removal,
  unambiguous 1:1 field renames.
- **Real drift, risky/ambiguous** → document only (semantic changes, restructured
  payloads, fields whose meaning is unclear).
- **False positive** → note why (const/helper Type, openapi generator noise, optional
  field legitimately absent from a sample).

Every batch of code fixes is gated by `go build ./...` **and** `go test ./...` — the
latter is mandatory because interface/mock breaks (`feedback_gameclient_interface_mocks`)
do not show up in `go build` alone.

### Reverse-direction guardrail

Add a test — sibling to the existing `TestLoadFromOpenAPIContainsAllHardcoded` (which
only catches client-action-not-in-openapi) — that fails when a `get_commands.json`
command has **no** client method/action, with an explicit ignore-list for intentionally
unsupported commands (e.g. `v2_*` endpoints, streaming `subscribe_*`). This makes
command-coverage drift a build-time failure instead of something that silently
accumulates until the next manual audit.

```
TestServerCommandsCoveredByClient:
  for cmd in get_commands.json.commands:
    assert cmd.name in clientCoverage OR cmd.name in ignoreList
```

The ignore-list is committed with a comment per entry justifying why it's unsupported, so
future additions are a deliberate, reviewed act.

## Deliverable

- **Findings report** appended to / finalized in this doc (or a sibling
  `2026-07-08-api-drift-audit-findings.md`): every finding by layer, with verdict and
  action-taken/deferred, and a closing **honest verdict** on `BuiltForAPIVersion` — either
  "structs + commands now verified at v0.473.0" or "verified except for the named
  unverified-no-source structs and deferred items X, Y, Z."
- **Code fixes** for the clear breaks.
- **Reverse guardrail test.**

## Out of Scope (YAGNI)

- Capturing new live response samples from the server (Layer 3 uses samples + openapi;
  no-source structs are honestly flagged, not probed).
- v2 API migration (`v2_*` endpoints) — tracked separately, added to ignore-list only.
- Refactoring `responses.go` structure beyond the drift fixes themselves.
- Payload-shape auto-generation / codegen — this round is an audit, not a generator.

## Risks

- **Openapi generator noise:** the schema may include fields/nullability the live server
  never emits, producing false drift. Mitigation: live samples win over openapi when both
  exist; openapi-only findings are treated as lower-confidence.
- **Dynamic payloads:** client payloads built conditionally can't be statically diffed —
  flagged for manual check rather than silently passed.
- **Mock breakage:** any new interface method breaks `pkg/agent` / `pkg/skills` mocks —
  caught only by `go test ./...`, which is mandatory in the DoD.
