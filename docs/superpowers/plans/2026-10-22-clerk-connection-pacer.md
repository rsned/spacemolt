# Clerk-token Login + Host-wide Connection Pacer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fleet agents authenticate with single-use Clerk ws-tokens instead of passwords, and every dial on the host goes through one rolling-window pacer that learns the server's `retry_after` from every signal — so ~90 agents cold-start in 1–2 minutes and reconnect storms never earn an IP block.

**Architecture:** A new leaf package `pkg/game/clerk` talks to the two Clerk-gated HTTP endpoints. `pkg/game.ReconnectGate` keeps its file-locked, host-wide shape but enforces two rolling 60 s budgets (dials, password logins) instead of a fixed cooldown. `pkg/game.Client` gains `LoginWithToken`, loses its private retry loops (the gate is the only retry), learns close codes 4001/4002/4003, and parks on 4001. `InitializeAgent` wires it all: Acquire → mint → dial → `login_token`, password only as metered fallback.

**Tech Stack:** Go 1.24+, `github.com/coder/websocket v1.8.14`, stdlib `net/http`/`net/http/httptest`, `syscall.Flock`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-22-clerk-connection-pacer-design.md`

## Global Constraints

- Go 1.24+ idioms (range-over-int, `b.Loop()` in benchmarks). `golangci-lint` must report no new findings after every task (`golangci-lint run ./...`).
- Every new duration constant lives in `pkg/game/constants.go` under the `Sleep*` block; no literal sleeps in `pkg/worker` (use the `settle(ctx, d)` seam if a worker-side wait is ever needed — this plan needs none).
- The Clerk key never appears in any error string, log line, status file, or `credentials.json`. Tests assert this where a key is in scope.
- Commits use explicit paths (`git add <files>`), never `git add -A` — `data/agents/*/schedule.json` are dirty in the working tree.
- Module path `github.com/rsned/spacemolt`. Binaries go under `bin/`.
- WebSocket v1 only (`wss://game.spacemolt.com/ws`); the `{type, payload, timestamp}` frame shape is unchanged.
- Wire shapes are those in the spec's appendix, not `server_docs/api.*.md`: `GET /api/registration-code` → `{registration_code, players:[{id, username, empire, hidden}]}`; `POST /api/player/{id}/ws-token` → `{token, expires_in}`; auth header `Authorization: Bearer <key>`.

## Review Focus

1. **A token minted, then a long gate wait, then dial** — the token is ~5 min single-use; if the fleet is inside a recorded block the token must not be minted before `Acquire` returns. Pinned in Task 6 (`TestDialWithGate_MintsAfterAcquire`).
2. **Gate file written by an old binary or truncated mid-write** — `{"last_attempt_nanos":…,"blocked_until_nanos":…}` or `{"dials":[` must load as "one historical dial, keep the block" / "empty", never panic or drop a live block. Pinned in Task 3 (`TestGate_ReadsLegacyFormat`, `TestGate_CorruptFileResetsToEmpty`).
3. **Clerk returns 401/403 (key revoked) on every mint** — the fleet must degrade to password login at ≤20/min, logging once per process, not once per attempt. Pinned in Task 6 (`TestDialWithGate_MintAuthFailureFallsBackToPasswordBudget`).
4. **4003 close reason variants** — `rate_limited retry_after=30`, `retry_after=30`, empty reason. Pinned in Task 4 (`TestCloseRetryAfter`).
5. **`clerk-sync` must not destroy a credentials file** — unknown fields preserved, 0600 kept, write is atomic, a username that matches two players (impossible today, but) refuses rather than guessing. Pinned in Task 2 (`TestSync_PreservesUnknownFieldsAndMode`, `TestSync_RefusesAmbiguousMatch`).

---

### Task 1: `pkg/game/clerk` — key loading and the two endpoints

**Files:**
- Create: `pkg/game/clerk/clerk.go`, `pkg/game/clerk/key.go`
- Test: `pkg/game/clerk/clerk_test.go`, `pkg/game/clerk/key_test.go`

**Interfaces:**
- Produces:
  ```go
  package clerk
  var ErrNoKey = errors.New("clerk: no API key configured")
  // LoadKey: $SPACEMOLT_CLERK_API_KEY, else the file named by $SPACEMOLT_CLERK_KEY_FILE,
  // else ~/.config/spacemolt/clerk_api_key. Whitespace trimmed. ErrNoKey when none is set/readable/empty.
  func LoadKey() (string, error)
  type Player struct { ID, Username, Empire string; Hidden bool }
  type Token struct { Value string; ExpiresIn time.Duration }
  type HTTPError struct { Status int; Method, URL string; RetryAfter time.Duration; Body string } // Body capped at 1024 bytes
  func (e *HTTPError) Error() string          // "clerk: POST /api/player/x/ws-token -> 429 (retry_after 7s)"; never includes the key
  func (e *HTTPError) RateLimited() bool      // Status == 429
  type Option func(*Source)
  func WithHTTPClient(c *http.Client) Option  // default: &http.Client{Timeout: 30 * time.Second}
  func WithBaseURL(u string) Option           // default "https://game.spacemolt.com"; trailing slash stripped
  func NewSource(key string, opts ...Option) *Source
  func (s *Source) ListPlayers(ctx context.Context) ([]Player, error)
  func (s *Source) RegistrationCode(ctx context.Context) (string, error)
  func (s *Source) MintWSToken(ctx context.Context, playerID string) (Token, error)
  ```
- Headers on every request: `Authorization: Bearer <key>`, `Accept: application/json`, `User-Agent: rsned/spacemolt-clerk/0.0.1` (a package-level `const userAgent`; `clerk` must not import `pkg/game` because `game` will import `clerk`).
- `RetryAfter` parsing on non-2xx: `Retry-After` header (integer seconds) first, else JSON body `retry_after` at the root or under `error`, else 0.
- `playerID` is `url.PathEscape`d.

- [ ] **Step 1: Write the failing tests** in `clerk_test.go` using `httptest.NewServer`:
  - `TestListPlayers_ParsesSpecShape` — server returns `{"registration_code":"379…","players":[{"id":"573b","username":"battle_bot1","empire":"nebula","hidden":false}]}`; assert one `Player{ID:"573b", Username:"battle_bot1", Empire:"nebula", Hidden:false}` and that the request carried `Authorization: Bearer k-test` and `Accept: application/json`.
  - `TestRegistrationCode` — same body; assert `"379…"`.
  - `TestMintWSToken_ReturnsTokenAndExpiry` — `POST /api/player/p%2F1/ws-token` returns `{"token":"t","expires_in":300}`; assert `Token{Value:"t", ExpiresIn:300*time.Second}` and method POST and the escaped path.
  - `TestMintWSToken_429HeaderBecomesHTTPError` — 429 with `Retry-After: 7`; assert `errors.As(err, &he)`, `he.Status==429`, `he.RetryAfter==7*time.Second`, `he.RateLimited()`, and `!strings.Contains(err.Error(), "k-test")`.
  - `TestMintWSToken_429BodyRetryAfter` — 429, no header, body `{"error":{"code":"rate_limited","retry_after":12}}`; assert `12*time.Second`.
  - `TestMintWSToken_EmptyTokenIsError` — 200 `{"token":""}`; assert error, not `*HTTPError`.
  - `TestMintWSToken_HTMLBodyIsBounded` — 503 with a 5 KB HTML body; assert `len(he.Body) <= 1024`.
  In `key_test.go` (use `t.Setenv`):
  - `TestLoadKey_EnvWins`, `TestLoadKey_FileFallbackTrimsWhitespace` (write `"  ak_x\n"` to a temp file named by `SPACEMOLT_CLERK_KEY_FILE`), `TestLoadKey_NoneIsErrNoKey` (env unset, file path points at a missing file).

- [ ] **Step 2: Run tests to verify they fail**
  Run: `go test ./pkg/game/clerk/ -run . -v`
  Expected: compile failure (package does not exist).

- [ ] **Step 3: Implement `key.go` and `clerk.go`** per the Interfaces block. One private `do(ctx, method, path string) ([]byte, error)` performs the request, builds `*HTTPError` on non-2xx, and bounds the body with `io.LimitReader(…, 1024)`.

- [ ] **Step 4: Run tests to verify they pass**
  Run: `go test ./pkg/game/clerk/ -v && golangci-lint run ./pkg/game/clerk/`
  Expected: all PASS, lint clean.

- [ ] **Step 5: Commit**
  ```bash
  git add pkg/game/clerk/
  git commit -m "feat(clerk): Clerk API key loader and ws-token/registration-code client"
  ```

---

### Task 2: `player_id` in credentials + `cmd/tools/clerk-sync`

**Files:**
- Modify: `pkg/game/agent.go:29-33` (Credentials struct)
- Create: `cmd/tools/clerk-sync/main.go`, `cmd/tools/clerk-sync/sync.go`
- Test: `cmd/tools/clerk-sync/sync_test.go`

**Interfaces:**
- Consumes: `clerk.Source.ListPlayers` (Task 1).
- Produces:
  ```go
  // pkg/game/agent.go
  type Credentials struct {
      Username string `json:"username"`
      Password string `json:"password"`
      Empire   string `json:"empire"`
      PlayerID string `json:"player_id,omitempty"` // Clerk-owned player id; empty = unknown
  }
  // cmd/tools/clerk-sync/sync.go
  type Result struct { Written, Unchanged []string; Unmatched []string /* agent dirs */; Orphans []string /* owned usernames with no dir */; Ambiguous []string }
  func Sync(agentsRoot string, players []clerk.Player, dryRun bool) (Result, error)
  ```
- `Sync` reads each `<agentsRoot>/*/credentials.json` into `map[string]any` (not the struct — unknown fields survive), matches on `username`, sets `player_id`, writes via temp file + `os.Rename` preserving the original file mode. Two players sharing a username → `Ambiguous`, file untouched. `LoadCredentials` keeps its three required-field checks; `PlayerID` is optional.
- `main.go` flags: `--agents data/agents`, `--dry-run`. Loads the key with `clerk.LoadKey()`, prints each `Result` list with counts, exits 1 if `len(Ambiguous) > 0`.

- [ ] **Step 1: Write the failing tests** in `sync_test.go` with a `t.TempDir()` agents root:
  - `TestSync_WritesPlayerIDForMatch` — one dir with `{"username":"a","password":"p","empire":"e"}`, players `[{ID:"1",Username:"a"}]`; assert file now has `"player_id":"1"`, `Result.Written==["dir"]`.
  - `TestSync_PreservesUnknownFieldsAndMode` — file created `0600` with an extra `"note":"keep"`; assert `note` still present and `Stat().Mode().Perm()==0600`.
  - `TestSync_UnchangedWhenIDMatches`, `TestSync_ReportsUnmatchedAndOrphans`, `TestSync_RefusesAmbiguousMatch` (two players with username `a`; file unchanged, `Ambiguous==["dir"]`), `TestSync_DryRunWritesNothing`.
  - In `pkg/game`: `TestLoadCredentials_PlayerIDOptional` — file without `player_id` loads; with it, `creds.PlayerID` populated. Add to the existing agent test file if one exists, else `pkg/game/agent_credentials_test.go`.

- [ ] **Step 2: Run tests to verify they fail**
  Run: `go test ./cmd/tools/clerk-sync/ ./pkg/game/ -run 'TestSync|TestLoadCredentials_PlayerID' -v`
  Expected: FAIL / does not compile.

- [ ] **Step 3: Implement** the struct field, `Sync`, and `main.go`.

- [ ] **Step 4: Run tests and lint**
  Run: `go test ./cmd/tools/clerk-sync/ ./pkg/game/ -run 'TestSync|TestLoadCredentials_PlayerID' && golangci-lint run ./cmd/tools/clerk-sync/ ./pkg/game/`
  Expected: PASS, lint clean.

- [ ] **Step 5: Dry-run against the real tree** (read-only; needs the key file from the spike)
  Run: `go run ./cmd/tools/clerk-sync --dry-run`
  Expected: `Written` ≈ 215, `Unmatched` lists `random-6 random-7 random-8 random-9 empire-crimson empire-crimson-real`, `Ambiguous` empty. Do **not** run without `--dry-run` yet — that is the rollout step (Task 9).

- [ ] **Step 6: Commit**
  ```bash
  git add pkg/game/agent.go cmd/tools/clerk-sync/ pkg/game/agent_credentials_test.go
  git commit -m "feat(clerk-sync): record Clerk player_id in credentials.json"
  ```

---

### Task 3: Gate — rolling-window budgets replace the fixed cooldown

**Files:**
- Modify: `pkg/game/reconnect_gate.go` (whole file), `pkg/game/constants.go` (add constants), `pkg/game/client.go:258-272` (delete `reconnectGateCooldown`, `reconnectBlockDefault`), `pkg/game/login_gate.go:35-45`, `pkg/game/agent.go:144,225`, `pkg/game/client.go:384-440` (attemptReconnection), `pkg/game/client.go:915-1000` (Reconnect)
- Test: `pkg/game/reconnect_gate_test.go` (rewrite), `pkg/game/login_gate_test.go` (signature updates only)

**Interfaces:**
- Produces:
  ```go
  // constants.go — inside the existing const block next to SleepIPRateLimitJitter
  ConnectWindow         = 60 * time.Second // server per-IP rate-limit window
  ConnectDialBudget     = 60               // dials per window (server: 100)
  ConnectPasswordBudget = 20               // password logins per window (server: 30)

  // reconnect_gate.go
  type DialKind int
  const ( DialToken DialKind = iota; DialPassword )
  type GateConfig struct { Window time.Duration; DialBudget, PasswordBudget int }
  // DefaultGateConfig: the three constants; $SPACEMOLT_CONNECT_BUDGET (positive int) overrides DialBudget.
  func DefaultGateConfig() GateConfig
  func NewReconnectGate(path string, cfg GateConfig) *ReconnectGate   // replaces (path, cooldown)
  func (g *ReconnectGate) Acquire(ctx context.Context, kind DialKind) error
  func (g *ReconnectGate) RecordBlock(d time.Duration, source string) error
  func (g *ReconnectGate) DefaultBlock() time.Duration                 // cfg.Window; nil-safe (returns ConnectWindow)
  type gateState struct {
      Dials             []int64 `json:"dials"`
      PasswordLogins    []int64 `json:"password_logins"`
      BlockedUntilNanos int64   `json:"blocked_until_nanos"`
      BlockSource       string  `json:"block_source,omitempty"`
      LastAttemptNanos  int64   `json:"last_attempt_nanos,omitempty"` // legacy read: folded into Dials on load
  }
  ```
- `tryClaim(kind)` algorithm: prune both slices to `now - Window`; if `LastAttemptNanos != 0`, append it to `Dials` and zero it; `readyAt := blockedUntil`; if `len(Dials) >= DialBudget` → `readyAt = max(readyAt, oldest(Dials)+Window)`; if `kind == DialPassword && len(PasswordLogins) >= PasswordBudget` → likewise with `PasswordLogins`; `minSpacing := Window / DialBudget`; `readyAt = max(readyAt, newest(Dials)+minSpacing)`; if `now >= readyAt` append `now` to `Dials` (and to `PasswordLogins` for `DialPassword`) and persist; else return `readyAt - now`.
- Jitter: `defaultJitter(minSpacing)` (half of it), as today.
- `recordRateLimitBlock(g, err, source string)`: uses `rateLimitBlock(err.Error(), g.DefaultBlock())`.
- `RecordBlock` logs via the standard logger whenever it extends the block: `log.Printf("gate block source=%s for=%s until=%s", source, d, until.Format(time.RFC3339))` — this is the always-on incident record the spec asks for (one line per block, host-wide, not per debug flag).
- Reconnect-path ownership (one `Acquire` per dial, done by the function that dials): `Client.Reconnect` loses its inner `maxRetries` loop and the fixed `2 s` pre-sleep and becomes **one** gated try — `g.Acquire(ctx, DialPassword)` (Task 6 changes the kind) → `Connect` → login → resync; failures return. `attemptReconnection` therefore no longer calls `Acquire` itself and drops its local exponential backoff; it keeps `reconnectMaxAttempts = 3` and simply calls `reconnectFn` up to three times (the gate's spacing/block inside `Reconnect` is the backoff).

- [ ] **Step 1: Rewrite `reconnect_gate_test.go`** keeping `fakeClock` and `newTestGate(t, path, cfg, clk)`. Tests (`cfg := GateConfig{Window: 60*time.Second, DialBudget: 6, PasswordBudget: 2}` so numbers stay small):
  - `TestGate_FirstAcquireIsImmediate`
  - `TestGate_MinSpacingIsWindowOverBudget` — two `Acquire(DialToken)`; `clk.slept == [10s]`.
  - `TestGate_DialBudgetExhaustedWaitsForOldest` — 6 acquires (advance clock 10 s each so spacing is satisfied), then a 7th; assert it slept until `first+60s`.
  - `TestGate_PasswordBudgetIsSeparate` — 2× `Acquire(DialPassword)` (spaced), a 3rd `DialPassword` waits ~60 s from the first, while a `DialToken` in between does not wait on the password budget.
  - `TestGate_PasswordAcquireCountsAsDialToo` — after 2 password acquires, `len(Dials)==2` in the file.
  - `TestGate_HonorsBlockFleetWide` and `TestGate_RecordBlockNeverShortens` (port existing; `RecordBlock(d, "test")` and assert `block_source` persisted).
  - `TestGate_ReadsLegacyFormat` — pre-write `{"last_attempt_nanos":N,"blocked_until_nanos":M}`; first `Acquire` waits for the block and treats N as one dial (spacing applies).
  - `TestGate_CorruptFileResetsToEmpty` — pre-write `{"dials":[`; `Acquire` succeeds without waiting and the file is valid JSON afterwards.
  - `TestGate_PrunesOldEntries` — advance clock 2 min; file has ≤1 entry after next claim.
  - `TestDefaultGateConfig_EnvOverridesDialBudget` — `t.Setenv("SPACEMOLT_CONNECT_BUDGET","30")` → `DialBudget==30`; `"abc"`/`"0"` → default 60.
  - `TestGate_NilIsNoop`, `TestGate_AcquireRespectsCanceledContext`, `TestRateLimitBlockParsing` (keep).
  Update `login_gate_test.go` only for the new `NewReconnectGate(path, cfg)` and `Acquire(ctx, kind)` signatures.

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./pkg/game/ -run 'TestGate|TestDefaultGateConfig|TestDialWithGate' -v`
  Expected: compile errors.

- [ ] **Step 3: Implement** the gate per Interfaces; add the three constants; delete `reconnectGateCooldown` and `reconnectBlockDefault`; update `agent.go:144,225` to `NewReconnectGate(DefaultReconnectGatePath(), DefaultGateConfig())`; update `recordRateLimitBlock`; simplify `Reconnect` and `attemptReconnection` as described (keep the post-login `drainPendingReplay`/`GetSystem`/`GetStatus` resync in `Reconnect`).

- [ ] **Step 4: Run the package tests and lint**
  Run: `go build ./... && go test ./pkg/game/ && golangci-lint run ./pkg/game/`
  Expected: PASS, clean. If `cmd/debug/login-probe` or other callers referenced the deleted constants, fix them to `game.ConnectWindow / time.Duration(game.ConnectDialBudget)`.

- [ ] **Step 5: Commit**
  ```bash
  git add pkg/game/reconnect_gate.go pkg/game/reconnect_gate_test.go pkg/game/login_gate.go pkg/game/login_gate_test.go pkg/game/constants.go pkg/game/client.go pkg/game/agent.go
  git commit -m "feat(gate): rolling-window dial and password budgets replace the fixed cooldown"
  ```

---

### Task 4: Close codes 4001/4002/4003

**Files:**
- Modify: `pkg/game/close_codes.go`, `pkg/game/client.go:817-835` (handleClose), `pkg/game/client.go:4028-4039` (ip_timed_out → gate)
- Test: `pkg/game/close_codes_test.go`

**Interfaces:**
- Produces:
  ```go
  const ( closeReplay closeAction = iota; closeFailFast; closeDisplaced; closeRateLimited )
  // String(): "displaced", "rate-limited"
  const ( CloseSessionReplaced websocket.StatusCode = 4001; CloseAuthTimeout = 4002; CloseConnectionRateLimited = 4003 )
  func closeRetryAfter(reason string) (time.Duration, bool) // "rate_limited retry_after=30" → 30s,true; "retry_after=5" → 5s,true; "" → 0,false
  func (c *Client) Displaced() bool
  func (c *Client) clearDisplaced()
  ```
- `knownCloseCodes` gains: `{4001, closeDisplaced, "session_replaced — another login owns this player; do not auto-reconnect"}`, `{4002, closeFailFast, "auth_timeout — reconnect and authenticate promptly"}`, `{4003, closeRateLimited, "connection_rate_limited — honor retry_after"}`.
- `handleClose`: `closeRateLimited` → `failPending` then `c.gate.RecordBlock(d, "close4003")` with `d` from `closeRetryAfter`, else `c.gate.DefaultBlock()`; `closeDisplaced` → `failPending`, set `c.displaced = true` (atomic.Bool), log at WARN via `log.Printf` (always-on) `"session_replaced: parking; one gated retry in %v"`.
- `parseErrorState` `ip_timed_out` branch: capture `seconds` and `c.gate` under the lock; after the function's deferred unlock path (restructure: compute inside, call `gate.RecordBlock(time.Duration(seconds)*time.Second, "ip_timed_out")` in a `defer` placed before `c.mu.Lock()` releases — simplest: hoist the gate call into a small helper invoked by the caller of `parseErrorState`; implementer's choice, but it must not hold `c.mu` during file I/O).

- [ ] **Step 1: Write the failing tests** in `close_codes_test.go`:
  - `TestClosePolicy_4001IsDisplaced`, `TestClosePolicy_4002IsFailFast`, `TestClosePolicy_4003IsRateLimited` — `lookupClosePolicy(code, "")` returns the action and `known == true`.
  - `TestCloseRetryAfter` — table: `"rate_limited retry_after=30"`→30 s; `"retry_after=5"`→5 s; `"rate_limited"`→(0,false); `""`→(0,false).
  - `TestHandleClose_4003RecordsBlockOnGate` — client with a test gate (temp path, fake clock); `handleClose(&websocket.CloseError{Code: 4003, Reason: "rate_limited retry_after=30"})`; read the gate file: `blocked_until_nanos == now+30s`, `block_source == "close4003"`.
  - `TestHandleClose_4003NoHintUsesDefaultBlock` — reason `""` → `blocked_until == now+60s`.
  - `TestHandleClose_4001SetsDisplaced` — `c.Displaced()` true afterwards; pending handles got `ConnectionClosed`.
  - `TestParseErrorState_IPTimedOutRecordsBlock` — payload `{"code":"ip_timed_out","message":"… in 120 seconds"}` → gate `blocked_until >= now+120s`, source `"ip_timed_out"`.

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./pkg/game/ -run 'TestClosePolicy|TestCloseRetryAfter|TestHandleClose|TestParseErrorState_IPTimedOut' -v`
  Expected: FAIL.

- [ ] **Step 3: Implement** per Interfaces.

- [ ] **Step 4: Run tests and lint**
  Run: `go test ./pkg/game/ && golangci-lint run ./pkg/game/`
  Expected: PASS, clean.

- [ ] **Step 5: Commit**
  ```bash
  git add pkg/game/close_codes.go pkg/game/close_codes_test.go pkg/game/client.go
  git commit -m "feat(close-codes): 4001 parks, 4002 reconnects, 4003 feeds retry_after to the gate"
  ```

---

### Task 5: Displaced-worker parking in `ReconnectingHandler`

**Files:**
- Modify: `pkg/game/client.go:321-372` (OnDisconnected, TriggerReconnect), `pkg/game/constants.go`
- Test: `pkg/game/client_reconnect_test.go` (new, or the file that already tests `attemptReconnection` with `reconnectFn` — find with `grep -ln reconnectFn pkg/game/*_test.go`)

**Interfaces:**
- Consumes: `Client.Displaced()`, `clearDisplaced()` (Task 4).
- Produces:
  ```go
  // constants.go
  SleepDisplacedRetry = 60 * SleepTick // 10min: parked after 4001 before ONE gated reconnect attempt
  // ReconnectingHandler
  displacedRetryDelay time.Duration // zero → SleepDisplacedRetry; tests set it tiny
  ```
- `OnDisconnected`: if `r.client.Displaced()`: do **not** touch `shortLivedCount`, do not call `attemptReconnection`; instead start one goroutine that waits `displacedRetryDelay` (or ctx), then `clearDisplaced()` and runs `attemptReconnection` with a single attempt. A second 4001 re-parks via the same path. Guard with the existing `reconnecting` CAS so two timers never run.
- `TriggerReconnect`: returns `false` immediately when `r.client.Displaced()` (so `nudgeReconnect` in `cmd/worker` is inert while parked).

- [ ] **Step 1: Write the failing tests** (drive with `reconnectFn` stub and `displacedRetryDelay = 10*time.Millisecond`):
  - `TestReconnectingHandler_DisplacedDoesNotReconnectImmediately` — mark displaced, call `OnDisconnected(err)`; within 5 ms `reconnectFn` call count is 0.
  - `TestReconnectingHandler_DisplacedRetriesOnceAfterDelay` — after 50 ms, `reconnectFn` called exactly once and `Displaced()` is false.
  - `TestReconnectingHandler_TriggerReconnectNoopWhileDisplaced` — returns false, `reconnectFn` count 0.
  - `TestReconnectingHandler_DisplacedDoesNotCountAsContention` — two displaced disconnects with sub-30 s uptime do not call `onSessionContention`.

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./pkg/game/ -run 'TestReconnectingHandler_Displaced|TestReconnectingHandler_TriggerReconnectNoop' -v`
  Expected: FAIL.

- [ ] **Step 3: Implement** per Interfaces.

- [ ] **Step 4: Run tests and lint**
  Run: `go test -race ./pkg/game/ -run 'TestReconnectingHandler' && golangci-lint run ./pkg/game/`
  Expected: PASS, clean.

- [ ] **Step 5: Commit**
  ```bash
  git add pkg/game/client.go pkg/game/constants.go pkg/game/client_reconnect_test.go
  git commit -m "feat(reconnect): park a displaced (4001) worker and retry once every 10 minutes"
  ```

---

### Task 6: Token login, mint-after-Acquire, and the fresh-start retry loop

**Files:**
- Modify: `pkg/game/client.go:648-775` (Connect), `pkg/game/client.go:1136-1162` (Login), `pkg/game/server_errors.go:14-26`, `pkg/game/terminator.go:86-92`, `pkg/game/login_gate.go`, `pkg/game/agent.go:118-200` (InitializeAgent), `pkg/game/agent.go:208-240` (InitializeMCPAgent — kind only)
- Test: `pkg/game/login_gate_test.go`, `pkg/game/client_login_test.go` (new)

**Interfaces:**
- Consumes: `clerk.Source`, `clerk.LoadKey`, `clerk.HTTPError` (Task 1); `Credentials.PlayerID` (Task 2); `Acquire(ctx, kind)`, `recordRateLimitBlock(g, err, source)` (Task 3).
- Produces:
  ```go
  // server_errors.go
  type ServerError struct { Code, Message string; RetryAfter time.Duration } // RetryAfter from details.retry_after / payload retry_after via rateLimitBucketFrom
  // client.go
  func (c *Client) LoginWithToken(ctx context.Context, token string) error // sends {"type":"login_token","payload":{"token":token}}; awaits logged_in; same timeout as Login
  // Connect: single dial attempt. On 429 returns fmt.Errorf("failed to connect: %w (%s)", err, rlDetail) with no internal retry/backoff.
  // login_gate.go
  type TokenMinter func(ctx context.Context) (string, error)
  type authAttempt struct {
      mint    TokenMinter                              // nil → password only
      connect func(ctx context.Context) error
      login   func(ctx context.Context, token string) error // token=="" → password
  }
  // dialWithGate: up to `attempts` gated tries. Per try: g.Acquire(ctx, DialToken) [DialPassword when mint==nil];
  // if mint != nil: tok, err := mint(ctx); on *clerk.HTTPError.RateLimited() → g.RecordBlock(RetryAfter or DefaultBlock, "mint429"), continue to next try;
  // on any other mint error → log once per process ("clerk mint failed (%v); falling back to password"), g.Acquire(ctx, DialPassword), tok = "".
  // then connect(ctx); on error recordRateLimitBlock(g, err, "connect"); continue. then login(ctx, tok); on error recordRateLimitBlock(g, err, "login"); continue.
  // Returns nil on first success, else the last error. attempts <= 0 → 1.
  func dialWithGate(ctx context.Context, g *ReconnectGate, attempts int, a authAttempt) error
  const dialAttempts = 3
  // agent.go
  func resolveTokenMinter(creds *Credentials, logger *log.Logger) TokenMinter // nil when no key; logs "auth=clerk player_id=…" or "auth=password reason=…" exactly once
  ```
- `resolveTokenMinter`: `key, err := clerk.LoadKey()`; `ErrNoKey` → nil. Else `src := clerk.NewSource(key)`; if `creds.PlayerID == ""`: `ListPlayers` once, match `Username`; no match → log `auth=password reason=not_owned` and return nil; match → log that `clerk-sync` should be run, use the id. Returns `func(ctx) (string, error) { t, err := src.MintWSToken(ctx, id); return t.Value, err }`.
- `InitializeAgent` step 4–6 becomes `dialWithGate(ctx, gate, dialAttempts, authAttempt{mint: resolveTokenMinter(creds, logger), connect: <existing connect+Ready closure>, login: func(ctx, tok) error { if tok != "" { return client.LoginWithToken(ctx, tok) }; return client.Login(ctx) }})`.
- `Client.Reconnect` (Task 3 simplified it) must also use the minter: store it on the client via `func (c *Client) SetTokenMinter(m TokenMinter)` (called by `InitializeAgent`); `Reconnect` does `Acquire(DialToken|DialPassword)` → mint (same fallback rules, using `recordRateLimitBlock`) → `Connect` → `LoginWithToken`/`Login`. Factor the shared per-try body so `dialWithGate` and `Reconnect` call the same function rather than duplicating the mint/fallback rules.
- `InitializeMCPAgent`: `dialWithGate(ctx, gate, 1, authAttempt{connect:…, login: func(ctx, _ string) error {…}})` — password kind, single attempt, unchanged behavior.

- [ ] **Step 1: Write the failing tests**
  `login_gate_test.go` (fake gate via temp path + fake clock, stub closures):
  - `TestDialWithGate_MintsAfterAcquire` — record call order; assert `[acquire, mint, connect, login]` and the login received the minted token.
  - `TestDialWithGate_Mint429RecordsBlockAndRetries` — mint returns `&clerk.HTTPError{Status:429, RetryAfter: 7*time.Second}` once then a token; assert gate file `block_source=="mint429"`, `blocked_until==now+7s`, and the second try succeeded with the gate having slept ≥7 s.
  - `TestDialWithGate_MintAuthFailureFallsBackToPasswordBudget` — mint returns `&clerk.HTTPError{Status:401}` every time; assert login got `""`, gate file has one entry in `password_logins`, and the fallback log line appears exactly once across 3 attempts (capture logger).
  - `TestDialWithGate_StopsAfterAttempts` — connect always fails with a non-rate-limit error; `reconnectFn`-style counter == 3; error returned.
  - `TestDialWithGate_NoMinterIsPasswordKind` — `password_logins` has one entry.
  - Keep/adapt the five existing `TestDialWithGate_*` cases.
  `client_login_test.go` (use the package's existing fake-conn/router test harness — see how `TestHandleClose_FailFastPropagates` builds a client):
  - `TestLoginWithToken_SendsLoginTokenFrame` — captured outbound message has `Type=="login_token"` and `Payload["token"]=="tok"`; `logged_in` response resolves nil.
  - `TestServerError_RetryAfterPopulated` — `serverErrorFromPayload({"code":"rate_limited","details":{"retry_after":9}})` → `RetryAfter==9s`.
  - `TestConnect_429ReturnsWithoutRetry` — `httptest` server answering 429 with `Retry-After: 30`; `Connect` returns in <1 s with an error containing `retry-after: 30`.
  `agent.go`:
  - `TestResolveTokenMinter_NoKeyIsNil` (`t.Setenv("SPACEMOLT_CLERK_API_KEY","")`, key file env pointing at a missing path).
  - `TestResolveTokenMinter_UsesPlayerIDWithoutListing` — `clerk` base URL pointed at an `httptest` server (via a package-level `var clerkBaseURL = ""` hook in `agent.go` passed as `clerk.WithBaseURL` when non-empty); the server must see **no** `/api/registration-code` request when `creds.PlayerID` is set.

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./pkg/game/ -run 'TestDialWithGate|TestLoginWithToken|TestServerError_RetryAfter|TestConnect_429|TestResolveTokenMinter' -v`
  Expected: FAIL / compile errors.

- [ ] **Step 3: Implement** per Interfaces. `Connect` keeps everything after the dial (read limit, goroutines) and loses only the `for attempt` loop.

- [ ] **Step 4: Run the full package with race detector and lint**
  Run: `go build ./... && go test -race ./pkg/game/ && golangci-lint run ./pkg/game/`
  Expected: PASS, clean.

- [ ] **Step 5: Commit**
  ```bash
  git add pkg/game/client.go pkg/game/server_errors.go pkg/game/terminator.go pkg/game/login_gate.go pkg/game/login_gate_test.go pkg/game/client_login_test.go pkg/game/agent.go pkg/game/agent_test.go
  git commit -m "feat(auth): Clerk ws-token login behind the gate, mint after Acquire, 3 gated fresh-start tries"
  ```

---

### Task 7: Status visibility, overmind knobs, worker wiring

**Files:**
- Modify: `pkg/overmind/control/messages.go:86-87`, `cmd/worker/main.go:740-760` (statusFlags/buildStatus), `pkg/overmind/supervisor/supervisor.go:424-455`, `cmd/overmind/main.go:37-38,98`, `pkg/overmind/supervisor/supervisor.go:217`
- Test: `cmd/worker/main_test.go` (or wherever `buildStatus` is tested — `grep -ln buildStatus cmd/worker/*_test.go`), `pkg/overmind/supervisor/supervisor_test.go`

**Interfaces:**
- Consumes: `Client.Displaced()` (Task 4).
- Produces:
  ```go
  // control.Status
  Displaced bool `json:"displaced,omitempty"` // 4001 session_replaced; parked, one gated retry per SleepDisplacedRetry
  // cmd/worker statusFlags
  Displaced bool
  // cmd/overmind
  stagger      := flag.Duration("stagger", 0, "Extra delay between initial worker launches (0 = let the host-wide connection gate pace)")
  connectBudget := flag.Int("connect-budget", 0, "Dials per minute for this host's connection gate (0 = game.ConnectDialBudget); exported to workers as SPACEMOLT_CONNECT_BUDGET")
  ```
- `cmd/overmind`: when `*connectBudget > 0`, `os.Setenv("SPACEMOLT_CONNECT_BUDGET", strconv.Itoa(*connectBudget))` before the supervisor spawns anything (children inherit the environment). `supervisor.NewSupervisor` default `StaggerInterval` becomes `0`.
- Worker: the heartbeat's `statusFlags.Displaced = client.Displaced()` wherever `Connected` is computed.
- Supervisor `Disconnected` branch: if `w.LastStatus.Displaced` and not yet logged for this agent, log once `"%s displaced (session_replaced); parked, NOT restarting — it retries via the gate every %s"`; otherwise existing behavior. No restart either way (already the case).

- [ ] **Step 1: Write the failing tests**
  - `TestBuildStatus_Displaced` — `statusFlags{Connected:false, Displaced:true}` → `Status.Disconnected && Status.Displaced`.
  - `TestSupervisor_DisplacedLogsOnceAndNeverRestarts` — feed a heartbeat with `Displaced:true` across several reap ticks; restart count 0; the displaced log line appears once.
  - `TestNewSupervisor_DefaultStaggerIsZero`.

- [ ] **Step 2: Run to verify failure**
  Run: `go test ./cmd/worker/ ./pkg/overmind/supervisor/ -run 'TestBuildStatus_Displaced|TestSupervisor_Displaced|TestNewSupervisor_DefaultStagger' -v`
  Expected: FAIL.

- [ ] **Step 3: Implement** per Interfaces.

- [ ] **Step 4: Build, test, lint**
  Run: `go build ./... && go test ./cmd/worker/ ./pkg/overmind/... ./cmd/overmind/ && golangci-lint run ./cmd/worker/ ./cmd/overmind/ ./pkg/overmind/...`
  Expected: PASS, clean.

- [ ] **Step 5: Commit**
  ```bash
  git add pkg/overmind/control/messages.go cmd/worker/main.go cmd/worker/main_test.go pkg/overmind/supervisor/supervisor.go pkg/overmind/supervisor/supervisor_test.go cmd/overmind/main.go
  git commit -m "feat(overmind): surface displaced workers, gate-paced launches, --connect-budget"
  ```

---

### Task 8: `login-probe --mode clerk`

**Files:**
- Modify: `cmd/debug/login-probe/main.go:311-345` (flags/dispatch) + a new `runClerk` function
- Test: none automated (live tool); verified by a live run in Task 9.

**Interfaces:**
- Consumes: `clerk.LoadKey`, `clerk.NewSource`, `MintWSToken` (Task 1); `game.Client.LoginWithToken` (Task 6); `LoadCredentials` → `PlayerID` (Task 2).
- Produces: `func runClerk(ctx context.Context, url string, creds *game.Credentials, count int, interval time.Duration, debug bool)` — for `i := range count`: mint, `NewClient(...).Connect`, wait `Ready()`, `LoginWithToken`, `Close`; print per-iteration `ok <ms>` or the error and stop at the first error. Summary: `N logins in Ts (R/min), 0 rate_limited`. New flags: `--count` (default 20). `--mode clerk` requires `creds.PlayerID != ""` (else print "run clerk-sync first" and exit 2). The header comment gains a paragraph describing the mode and that it mints one token per login.

- [ ] **Step 1: Implement `runClerk` and the flag/dispatch changes.**
- [ ] **Step 2: Build and lint**
  Run: `go build -o bin/login-probe ./cmd/debug/login-probe && golangci-lint run ./cmd/debug/login-probe/`
  Expected: builds, clean.
- [ ] **Step 3: Commit**
  ```bash
  git add cmd/debug/login-probe/main.go
  git commit -m "feat(login-probe): clerk mode — paced ws-token logins"
  ```

---

### Task 9: Runbook, docs, and the live verification sequence

**Files:**
- Modify: `docs/COLD_START.md:130-145` (pacing section) and every `--stagger 10s` occurrence; `docs/superpowers/specs/2026-10-22-clerk-connection-pacer-design.md` (status line → "approved, implemented <date>")
- Create: none

**Steps:** (operator-facing; the implementer writes the docs, the session owner runs the live steps)

- [ ] **Step 1: Rewrite the COLD_START pacing section**: one Clerk key (`SPACEMOLT_CLERK_API_KEY` or `~/.config/spacemolt/clerk_api_key`), `go run ./cmd/tools/clerk-sync` once after any new account is registered/claimed, fleets may launch back-to-back, `--connect-budget` is the one knob (default 60/min; the server allows 100), what `auth=clerk`/`auth=password` and `gate block source=…` log lines mean, and that a `displaced` worker is parked on purpose. Remove `--stagger 10s` from every launch command and the "one fleet at a time" paragraph.
- [ ] **Step 2: Commit docs**
  ```bash
  git add docs/COLD_START.md docs/superpowers/specs/2026-10-22-clerk-connection-pacer-design.md
  git commit -m "docs(cold-start): Clerk-token login and the connection gate replace --stagger"
  ```
- [ ] **Step 3 (live, fleet stopped): `go run ./cmd/tools/clerk-sync`** — expect ~215 written, the six known unmatched dirs.
- [ ] **Step 4 (live, fleet stopped): `bin/login-probe --mode clerk --account battle_bot1 --count 20 --interval 1s`** — expect 20 `ok`, 0 `rate_limited`, summary ≈ 60/min.
- [ ] **Step 5 (live): full stop of every overmind, `go build -o bin/worker ./cmd/worker && go build -o bin/overmind ./cmd/overmind`, relaunch the smallest fleet first.** Watch its first worker log `auth=clerk player_id=… Ready!` and the overmind log for any `gate block`. Then launch the rest back-to-back. Record first-launch → last `Ready!` time per fleet; expect < 2 min for a ~20-worker fleet.
- [ ] **Step 6: Regenerate the Clerk key on the website** (it was pasted into a chat transcript during the spike), update `~/.config/spacemolt/clerk_api_key`, and restart one fleet to confirm the new key works. Do this only after Step 5 — regeneration invalidates the old key immediately.
- [ ] **Step 7 (session owner): update memory** — `reference_login_vs_reconnect_gating.md` (fresh logins are gated; budget model), add a pointer for the Clerk key location and `clerk-sync`, then `make memory-sync` and commit `docs/memory` per the standing rule.
