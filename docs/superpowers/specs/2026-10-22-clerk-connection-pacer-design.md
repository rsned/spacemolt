# Clerk-token login + host-wide connection pacer — design

Date: 2026-10-22. Status: draft, awaiting approval.

## Intent

Fleet cold start takes 15–30 minutes and reconnect storms have earned the
host escalating per-IP blocks (2 min → 30 min). Both come from the same
root: every account authenticates with a password, so all ~90 live agents
share one per-IP `session_auth` budget (30/min), and our pacing is a fixed
5 s host-wide cooldown tuned by folklore rather than to the server's actual
limits. When a block does land, parts of the stack retry inside it, which
extends it.

The official TypeScript client (`SpaceMolt/spacemolt-lib`) encodes the
server's real model. Port its *connection-management* layer to Go and wire
it under our existing v1 WebSocket client. Do **not** port the rest of the
lib (v2 protocol, generated command surface, state cache) — a spike on
2026-10-22 proved `/ws` (v1) accepts `login_token`, so the protocol our
6,300-line `pkg/game.Client` speaks does not have to change.

## Server facts this design is built on

Verified from the lib's source/notes and the 2026-10-22 spike (gameserver
v0.645.0). Numbers are server defaults; the pacer makes them configurable.

| Budget | Scope | Limit | How a rejection arrives |
|---|---|---|---|
| `connection` (WS upgrade) | per IP | 100/min | HTTP 429 on the handshake (body may carry `retry_after`), or WS close **4003** `connection_rate_limited` with `retry_after=<seconds>` in the close reason |
| `session_auth` (password `login`, `register`) | per IP, shared by every account | 30/min | WS `error` frame `code=rate_limited`, `details.retry_after` (s) |
| `clerk_login_token` (`login_token`) | **per player** | 10/min | same `rate_limited` frame |
| ws-token minting (`POST /api/player/{id}/ws-token`) | per Clerk user, separate from gameplay | (not published) | HTTP 429 |
| IP block (earned by accumulating any of the above) | per IP | escalates 2→30 min | WS `code=ip_timed_out` with countdown; HTTP 429 `limit: "ip_timeout"` |

Other close codes (server_docs/api.20261016.md §"Application Close Codes"):
**4001** `session_replaced` — another login took the slot; the server says do
not auto-reconnect. **4002** `auth_timeout` — upgraded but never
authenticated in the window; the server says *reconnect and authenticate
promptly* (so 4002 is **not** terminal — it is a plain reconnect). Code 1000
with reasons `max session age` / `inactivity` / `rolling restart` stays as
today (replay pending mutations, reconnect).

Clerk endpoints (verified live 2026-10-22; `server_docs/api.*.md` is out of
date on these — it describes a JWT and different field names. Trust this):
- Credential: a long-lived **API key** (`ak_…`, generated on the website,
  regenerating replaces it), sent as `Authorization: Bearer <key>`.
- `GET /api/registration-code` → `{"registration_code": "...", "players": [{"id","username","empire","hidden"}]}` — 225 players owned; 166 of the 171 live agents.
- `POST /api/player/{id}/ws-token` → `{"token": "<64 chars>", "expires_in": <n>}`; single-use, ~5 min TTL. Mint one per (re)connect; never cache. Measured: 70 mints in 34 s from one key with no 429 and no rate-limit headers, so the mint budget is ≥125/min — above our dial budget, not the binding limit.
- No `X-RateLimit-*` headers on any of these responses.

Login frame on v1: `{"type":"login_token","payload":{"token":"…"},"timestamp":…}` → `logged_in` (same payload shape as password login). On `/ws/v2` the same token also works (`tool: spacemolt_auth, action: login_token`).

## Goals

1. Cold start of the live fleet (~90 agents) in ≈1–2 minutes; 225 accounts in ≈4–5 minutes.
2. After a server restart drops every socket, the fleet reconnects through the same pacer, at the same budget, with no process racing another.
3. Any server back-off hint (`retry_after` in a 429 body, a 4003 close reason, a `rate_limited` frame, an `ip_timed_out` countdown) is honored exactly, fleet-wide, and nothing dials inside it.
4. One secret (the Clerk API key) replaces per-account passwords on the hot path; passwords remain for `register`, `claim`, and unowned accounts.

## Non-goals

- Migrating to `/ws/v2` or the `{tool, action}` protocol.
- Porting the lib's state cache, typed events, market/observation subscriptions, or generated command facade.
- Changing the MCP transport (`MCPGameClient`); it keeps password login and the shared gate as today.
- Tuning game-command (`game_query`/`game_mutation`) rate limits — out of scope; this is connection/auth only.

## Anti-goals (would make this a failure even if it "works")

- Any path that retries a dial or login *inside* a server-stated block. The current lockouts came from exactly that.
- A second, parallel pacing mechanism. Fresh logins, reconnects, `play_as`, and tools must all consume the one host-wide budget.
- Storing the Clerk key in the repo, in `data/agents/*/credentials.json`, in logs, or in `onSend`-style frame dumps.

## Design

### 1. Package `pkg/game/clerk`

Small, dependency-free (net/http, encoding/json).

```go
type Player struct { ID, Username, Empire string; Hidden bool }

type Source struct { /* key, httpBase, *http.Client, timeout */ }

func NewSource(key string, opts ...Option) *Source
func LoadKey() (string, error)           // SPACEMOLT_CLERK_API_KEY, else ~/.config/spacemolt/clerk_api_key
func (s *Source) ListPlayers(ctx) ([]Player, error)      // GET /api/registration-code
func (s *Source) RegistrationCode(ctx) (string, error)
func (s *Source) MintWSToken(ctx, playerID string) (token string, expiresIn time.Duration, err error)
```

- Every request sets `Authorization: Bearer <key>`, `Accept: application/json`, our `UserAgent`, and a 30 s timeout.
- A non-2xx returns `*HTTPError{Status, URL, RetryAfter time.Duration, Body string}` with `Retry-After` header / body `retry_after` parsed. The caller (pacer) decides what to do with it; this package never sleeps or retries.
- The key is never included in any error string or log line.

### 2. Credentials: `player_id` in `credentials.json`

`game.Credentials` gains `PlayerID string \`json:"player_id,omitempty"\``. Loading tolerates its absence.

A new tool `cmd/tools/clerk-sync` (one-shot, run by an operator):
1. `ListPlayers()` once.
2. For every `data/agents/*/credentials.json`, match on `username`; write `player_id` if missing or different (preserving all other fields, atomic rename).
3. Print: matched, newly written, unchanged, and **unmatched** agent dirs (these are the accounts not owned by the key — they stay on password login), plus owned players with no agent dir.

`credentials.json` is git-ignored (`.gitignore:113`), so this touches no tracked file.

### 3. Credential resolution in the client

`NewClient` keeps its `(url, username, password)` signature. A new setter installs the token path:

```go
type TokenMinter func(ctx context.Context) (string, error)
func (c *Client) SetTokenMinter(m TokenMinter)
```

**Mint before dial.** The token is minted *before* the WebSocket is opened
(it lives ~5 min, the dial+welcome+login takes well under a second), so a slow
or failing Clerk call can never leave an open, unauthenticated socket that the
server closes with 4002. Concretely `dialWithGate`'s connect step becomes
"mint, then dial"; `Login` receives the token it should send.

`Client.Login`:
- If a token is present: send `login_token`, await `logged_in`. On `rate_limited` from the server, surface it as `*ServerError` with `RetryAfter` populated (the existing `rate_limit_bucket.go` parsing already extracts it) — the caller paces; `Login` itself does not loop.
- If minting failed with a non-rate-limit error (key revoked, player not owned, 5xx): log once at WARN and fall back to password login **for this attempt only**, so a Clerk outage degrades gracefully instead of a dead fleet. The fallback consumes the gate's `password` budget (§4), so an outage cannot turn into 60 password logins/min. If minting failed with a rate-limit error (HTTP 429), return it — no fallback, and the 429 feeds the gate (§4).
- If no token and no minter: password login, as today.

`InitializeAgent` resolves the minter: `LoadKey()` succeeds **and** `creds.PlayerID != ""` → minter is `source.MintWSToken(ctx, creds.PlayerID)`. If the key loads but `PlayerID` is empty, call `ListPlayers()` once, match by username, and use that id for the process lifetime (log that `clerk-sync` should be run). If no key: password, with a one-line INFO so operators can see which mode a worker booted in.

`InitializeMCPAgent` is unchanged.

### 4. The pacer: `ReconnectGate` becomes a rolling-window budget

Keep the file-locked, host-wide design (it is the right primitive for N overmind processes on one IP). Change what it enforces:

**State file** (`/tmp/spacemolt-reconnect.gate`, overridable as today):
```json
{ "dials": [<unix-nanos>...], "password_logins": [<unix-nanos>...],
  "blocked_until_nanos": 0, "block_source": "" }
```
Two rolling counters, because the server has two per-IP budgets that
survive this change: `dials` (every WS upgrade — the `connection` bucket)
and `password_logins` (every password `login`/`register` — the
`session_auth` bucket, which token logins do not touch). Entries older than
`window` are pruned on each claim.

**Policy** (constructor options, with defaults):
- `window` = 60 s. `dialBudget` = **60** per window (server: 100). `passwordBudget` = **20** per window (server: 30). The margins cover traffic the pacer does not see (browser play, HTTP catalog/map fetches, anything not using `pkg/game`) and the fact that an overrun now escalates. Tunable per launch via `--connect-budget` on `cmd/overmind` and `cmd/worker` (passed through as an env var so every child agrees on the same number).
- `minSpacing` = `window / dialBudget` (1 s at 60/min) — the lib's rule that a full batch can never complete in under one window. Replaces the fixed `reconnectGateCooldown = 5s`.
- Jitter: up to `minSpacing / 2`, as today's `defaultJitter`.

**`Acquire(ctx, kind)`** where `kind` is `DialToken` or `DialPassword`: wait out `blocked_until`; then, if the window is full for `dials` (and, for `DialPassword`, also for `password_logins`), sleep until the oldest expires; then enforce `minSpacing` from the newest dial; then append `now` to the relevant counters and return. Existing callers pass `DialToken` when a token was minted and `DialPassword` otherwise (`InitializeMCPAgent`, `register`, `claim`, `play_as` on an unowned account).

**`RecordBlock(d, source)`**: as today but records which signal produced it (`"http429"`, `"mint429"`, `"close4003"`, `"rate_limited:connection"`, `"rate_limited:session_auth"`, `"ip_timed_out"`) for the log line. When a signal carries **no** duration, the default is one `window` (60 s), replacing today's `reconnectBlockDefault = 5 min`: a bare 429 on a cold start must not idle the whole fleet for five minutes.

**Signals that feed it** (the port's main correctness work):
| Where | Signal | Action |
|---|---|---|
| `clerk.MintWSToken` | HTTP 429 (`Retry-After` header or body `retry_after`) | `RecordBlock(retry_after, "mint429")` — one key serves the whole fleet, so a mint throttle is fleet-wide by nature. Measured headroom (≥125/min) means this should not fire at a 60/min dial budget; the row exists so that if it does, the response is correct |
| `Client.Connect` dial error | HTTP 429, body `retry_after` (already parsed by `rateLimitDetail`) | `RecordBlock(retry_after)`; return immediately — **remove** the in-`Connect` exponential retry loop entirely; the caller's gated retry is the only retry |
| `handleClose` | 4003, reason `rate_limited retry_after=N` | `RecordBlock(N s, "close4003")`, then reconnect via the handler as for an abnormal close |
| `handleClose` | 4002 `auth_timeout` | plain reconnect (fail pending, handler reconnects through the gate). With mint-before-dial this should be rare; log at WARN so a recurrence is visible |
| `handleClose` | 4001 `session_replaced` | new `closeDisplaced` action: fail pending, **do not** reconnect, mark the client `Displaced` (sticky — `RequestReconnect`/`nudgeReconnect` are no-ops while set), report `displaced: true` in the worker's heartbeat `control.Status`. What happens next is the **open decision** below. The existing `SessionContention` detector (two sub-30 s sessions → `os.Exit(2)`) stays as a fallback for servers that close with 1000 instead of 4001, but a 4001 never counts toward it |
| `Login` | `rate_limited` with `details.limit == "connection"` or `"session_auth"` | `RecordBlock(retry_after)`; `clerk_login_token` (per player) does **not** record a fleet-wide block — it waits `retry_after` locally and re-mints |
| any command | `ip_timed_out` with countdown | `RecordBlock(countdown, "ip_timed_out")` — today only the worker loop aborts on it; the gate should learn it too |

**Displaced (4001) policy — decided by the owner 2026-10-22.** Today a
displaced worker dies after two short sessions and the supervisor relaunches
it, which is the `session_replaced` thrash the runbook warns about when
`play_as` is used on a supervised agent. New behavior: the worker stays
alive and parked; the supervisor does not relaunch a worker reporting
`displaced`; after `SleepDisplacedRetry` (10 min, new constant in
`pkg/game/constants.go`) the worker makes **one** gated reconnect attempt
and re-parks on another 4001. An operator's `play_as` session therefore
displaces the bot cleanly and the bot returns on its own within ten minutes
of the operator leaving, with no kick-fight and at most one dial per ten
minutes per displaced worker. The `play_as` runbook note ("stop the worker
first") becomes unnecessary once this ships.

**Fresh start retries.** `InitializeAgent` currently makes a single gated
attempt and the worker exits fatally on failure, leaving the supervisor
relaunch (1 per 5 s reap tick, counted against `MaxRestarts`) as the only
retry. With `Connect`'s internal loop removed, `dialWithGate` instead loops
up to `reconnectMaxAttempts` (3) gated attempts — each `Acquire` honors any
block the previous attempt recorded — before returning the error. Supervisor
relaunch remains the backstop, not the mechanism.

**`Reconnect` / `attemptReconnection`**: keep the structure, but every attempt (not just the first) goes through `Acquire`, the fixed `2 s` pre-sleep and the inner `maxRetries = 3` loop in `Client.Reconnect` are removed (the handler's gated loop is the one retry mechanism), and a recorded block is honored by `Acquire` rather than by local exponential backoff.

### 5. Supervisor and runbook

- `cmd/overmind --stagger` default drops to `0` (meaning "let the gate pace"); the flag stays for operators who want extra spacing. `RestartBatch` stays at 1 per reap tick as a belt-and-braces cap.
- `docs/COLD_START.md`: remove the "one fleet at a time" and `--stagger 10s` rules; replace with: set `SPACEMOLT_CLERK_API_KEY` or the key file, run `clerk-sync` once, launch fleets back-to-back. Note the `--connect-budget` knob and that 60/min is the default.
- Memory note `reference_login_vs_reconnect_gating.md` is stale (fresh logins *are* gated via `dialWithGate`); update it when this ships.

### 6. Observability

- Each worker logs one line at boot: `auth=clerk player_id=… ` or `auth=password reason=…`.
- The gate logs every `RecordBlock` with source and duration via the always-on event logger (the same channel `rate_limited bucket=…` uses), so an incident tally is `grep 'gate block'`.
- `clerk-sync` output is the ownership record.

## Constraints

- Go 1.24+ idioms; `golangci-lint` clean; no `time.Sleep` in `pkg/worker` (use the `settle` seam); any new sleep constant lives in `pkg/game/constants.go`.
- The gate file format changes shape. New code reads the old `{last_attempt_nanos, blocked_until_nanos}` file (treat `last_attempt` as one dial, keep `blocked_until`) so a live block survives the deploy. Old binaries must **not** share the file with new ones: an old binary rewrites the file in the old shape on every claim, erasing `dials` and the rolling window. Therefore the rollout is a **full stop of every fleet, then fleet-by-fleet relaunch on the new binary** — no mixed-binary period. The first relaunched fleet is the canary (watch its first worker's `auth=clerk … Ready!` line and the gate log before launching the next), and the relaunch itself is the first measurement of Goal 1.
- The gate keys the `dials` window on wall-clock nanos as today; processes on one host share a clock, so no monotonic concerns.
- The Clerk key was pasted into a chat transcript during the spike. Regenerate it on the website once the fleet runs on the new binary (regeneration invalidates the old key, so not before).

## Verification

- Unit: gate rolling window (both counters, budget exhaustion, pruning, min-spacing, old-format read, block precedence, no-duration default) with injected clock; `clerk` package against an `httptest` server (200, 429 with header, 429 with body, 5xx, malformed); mint-before-dial ordering and `Login` dispatch (token / mint rate-limited / mint failed → password budget / no key); `lookupClosePolicy` for 4001/4002/4003 including `retry_after` parse; `Displaced` stickiness against `RequestReconnect`; `dialWithGate` retry loop honoring a recorded block.
- Live, before fleet rollout: `cmd/debug/login-probe` gains a `--clerk` mode: 20 token logins at 1 s spacing from this host → expect 20 `logged_in`, zero `rate_limited`. Run only with the fleet stopped.
- Live, rollout: time from first overmind launch to last worker `Ready!` for one fleet; expect under 2 minutes for ~20 workers.

## Left to the builder

- Exact option/constructor shapes in `pkg/game/clerk` and the gate; whether `budget`/`window` live as `Sleep*`-style constants or gate options (constants file is the convention for durations).
- How `cmd/worker` receives the key path/budget (env var inherited from the overmind is the suggested route).
- Whether `clerk-sync` also writes `empire` corrections when the server's value differs from the file (report only is acceptable for v1).

## Decisions already made (do not re-open)

- Port the connection layer only; stay on `/ws` v1.
- Package lives in this repo (`pkg/game/clerk` + changes to `pkg/game`), not a standalone module.
- `credentials.json` records `player_id`, never a `claimed` flag and never the key.
- Default budgets 60 dials/min and 20 password logins/min, not the server's 100 and 30.
- Password login remains as fallback and for `register`/`claim`, metered by its own counter in the same gate.
- Token is minted before the dial, never after.
- 4002 reconnects (server guidance); 4003 records the hint and reconnects; 4001 parks the worker with one gated retry every 10 minutes.
- Deploy is a full stop, then fleet-by-fleet relaunch on the new binary.
- Bare rate-limit signals with no duration record a 60 s block, not 5 min.
- Fresh-start retry lives in `dialWithGate` (3 gated attempts), supervisor relaunch is the backstop.

## Appendix: spike record (2026-10-22, gameserver v0.645.0)

Throwaway program in the session scratchpad; agent `battle_bot1`
(`player_id 573b6ead06d7f4aff3f1e02cdab75cf7`). Observed, tokens redacted:

```
GET  /api/registration-code                       -> 200  players=225  registration_code=379…ad7
POST /api/player/573b…cf7/ws-token                -> 200  {"token": <64 chars>, "expires_in": …}
/ws     welcome(version 0.645.0) -> {"type":"login_token","payload":{"token":…}} -> logged_in {unread_chat, player, ship, modules, system, poi}
/ws/v2  welcome -> {"tool":"spacemolt_auth","action":"login_token",…}           -> reconnected, logged_in (request_id echoed)
mint probe: 70 × POST ws-token in 33.7 s -> all 200, no Retry-After / X-RateLimit headers
ownership: 171 live agents -> 166 owned; unowned random-6, random-8, random-9 (+ random-7, empire-crimson, empire-crimson-real among non-live dirs)
```
