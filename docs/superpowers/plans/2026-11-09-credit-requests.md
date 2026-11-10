# Credit Requests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A worker whose station refuel fails for lack of money files a credit request on a shared queue; the_banker's worker pays it (20k) within ~10 minutes, under a 1-per-24h and 1M/day guard, with every payment and hold recorded and alerted.

**Architecture:** New `pkg/creditreq` is a flock-guarded JSON queue + JSONL history, modelled on `pkg/rescue/queue.go`. Workers file through a `game.GameClient` decorator that overrides only `Refuel` (no interface change, no signature threading). the_banker runs a new scheduled worker verb `pay_credit_requests` inside its live session. fleet-watch reads the queue and history for alerts.

**Tech Stack:** Go 1.24, `syscall.Flock` (as pkg/rescue), existing worker dispatch/scheduler, fleet-watch.

**Spec:** `docs/superpowers/specs/2026-11-09-credit-requests-design.md` (incl. its "Builder-check rulings" section)

## Global Constraints

- Queue file `data/overmind/credit-requests.json`; history `data/overmind/credit-requests-history.jsonl`.
- Amount **20,000**; trigger credits **< 5,000** after a fresh `GetStatus`; refuel error must contain **both** `no_fuel_source` and `insufficient credits`; agent docked.
- One payment per agent per **24 h**; **1,000,000** per UTC day fleet-wide; banker pays nothing when its own credits are **< 1,000,000**; a record whose gift fails **3** times becomes `held`.
- Statuses: `pending`, `held` (open, block new filing); `paid` leaves the queue (history only).
- Warnings: any `held`; `pending` older than **30 min**; agent paid **≥ 2** times in **7 days**.
- Filing is best-effort: a queue error is logged and never changes the `Refuel` return value.
- No new logins, no play_as: payment runs in the_banker worker's session. Never `time.Sleep` in pkg/worker.
- `go build ./... && go test <touched pkgs>`; `golangci-lint` adds no findings; commit with `--no-verify`, named files only, Co-Authored-By trailer.
- Adding methods to `game.GameClient` is NOT allowed (breaks mocks); the decorator embeds the interface.

## Review Focus

1. **Two workers filing at once / banker paying while a worker files** — no lost or duplicated record (flock covers read-modify-write). Test in Task 1.
2. **Refuel failing for another reason (dry station desk, docking error)** — no request filed. Test in Task 2.
3. **Unknown username at payment** (record has none, agents table has none) — record held, not paid to a wrong name, not retried forever. Test in Task 3.
4. **Banker crosses midnight UTC / history file missing or with a malformed line** — daily cap resets at UTC day; a bad history line is skipped, not fatal. Test in Task 1 (history) and Task 3 (cap).
5. **Gift succeeds but the queue write after it fails** — the record must not be paid twice: append history first, then transition; on restart a `pending` record with a matching history `paid` line in the last 24 h is held, not re-paid. Test in Task 3.

---

### Task 1: `pkg/creditreq` queue and history

**Files:**
- Create: `pkg/creditreq/queue.go`, `pkg/creditreq/history.go`, `pkg/creditreq/queue_test.go`, `pkg/creditreq/history_test.go`

**Interfaces:**
- Produces:
  - `type Status string`; `StatusPending = "pending"`, `StatusHeld = "held"`, `StatusPaid = "paid"`.
  - `type Record struct { AgentID, Username, BaseID, SystemID, Reason, HeldReason string; Credits, Fuel, MaxFuel float64; Status Status; RequestedAt, PaidAt time.Time; Amount int64; Attempts int }` (JSON snake_case tags).
  - `type Queue struct{…}`; `func NewQueue(path string) *Queue`.
  - `func (q *Queue) List() ([]Record, error)` — missing file = empty, no error.
  - `func (q *Queue) File(rec Record) (filed bool, err error)` — no-op (false) when the agent already has an open (`pending`/`held`) record; sets `Status=pending`, `RequestedAt` if zero.
  - `func (q *Queue) Update(agentID string, mutate func(*Record)) (bool, error)` — locked read-modify-write of that agent's record.
  - `func (q *Queue) Remove(agentID string) error`.
  - `type Entry struct { At time.Time; AgentID, Username, Event string; Amount int64; Detail string }` — Event ∈ `filed|paid|held|send_failed`.
  - `type History struct{…}`; `func NewHistory(path string) *History`; `func (h *History) Append(e Entry) error`; `func (h *History) Since(t time.Time) ([]Entry, error)` — skips malformed lines.
  - `func PaidSince(entries []Entry, agentID string, t time.Time) int` and `func TotalPaidSince(entries []Entry, t time.Time) int64`.

- [ ] **Step 1: Failing tests** — `TestFile_OneOpenRecordPerAgent` (second File for same agent returns false; after `Remove`, File returns true); `TestFile_HeldBlocksFiling`; `TestList_MissingFileIsEmpty`; `TestConcurrentFile` (20 goroutines × distinct agents via two `NewQueue` on the same path → 20 records); `TestUpdate_UnknownAgent` (false, nil); `TestHistory_SkipsMalformedLine` (file with a junk line between two valid ones → `Since` returns 2); `TestPaidSince_24hWindow` (paid at now-25h and now-1h → `PaidSince(now-24h)` = 1); `TestTotalPaidSince_UTCDay` (two 20k paid entries at 23:59Z and 00:01Z next day → total since that day's 00:00Z = 20000).
- [ ] **Step 2:** `go test ./pkg/creditreq` → FAIL (package missing).
- [ ] **Step 3:** Implement; locking exactly as `pkg/rescue/queue.go` `withLock` (lock file `<path>.lock`, write temp + rename).
- [ ] **Step 4:** `go test -race ./pkg/creditreq && golangci-lint run ./pkg/creditreq/...` → PASS, clean.
- [ ] **Step 5:** Commit `pkg/creditreq/`: "creditreq: shared credit-request queue and payment history".

### Task 2: Worker files a request when it cannot pay for fuel

**Files:**
- Create: `pkg/worker/credit_request.go`, `pkg/worker/credit_request_test.go`
- Modify: `cmd/worker/main.go` (flag + wrap the client before it is handed to dispatch/autopilot)

**Interfaces:**
- Consumes: `creditreq.Queue.File`, `creditreq.History.Append` (Task 1).
- Produces:
  - `type CreditFiler interface { File(creditreq.Record) (bool, error) }`
  - `type CreditRequestingClient struct { game.GameClient; Queue CreditFiler; History *creditreq.History; AgentID, Username string; Out io.Writer }`
  - `func (c *CreditRequestingClient) Refuel(ctx context.Context) error` — calls the embedded `Refuel`; when the error text contains both `no_fuel_source` and `insufficient credits`, the state says docked, and after `GetStatus` the credits are < 5000, files a record (BaseID/SystemID/Credits/Fuel/MaxFuel/Reason from state and error) and appends a `filed` history entry; logs `credit request filed (credits N)` once per filing. Always returns the original error unchanged.
  - Worker flag `--credit-requests` (default `data/overmind/credit-requests.json`, empty disables); history path = same dir, `credit-requests-history.jsonl`. Username from `data/agents/<id>/credentials.json` field `username` (empty if unreadable).

- [ ] **Step 1: Failing tests** with a fake `game.GameClient` (embed an existing test fake from pkg/worker; grep `type fakeClient` / `mockClient`) and a fake filer:
  - `TestRefuel_InsufficientCreditsFiles` — refuel err `no_fuel_source: No fuel cells in cargo and insufficient credits for station refueling`, docked, credits 0 → one record with AgentID, Username, Credits 0, Reason containing `no_fuel_source`; returned error is the original.
  - `TestRefuel_DryDeskDoesNotFile` — err `no_fuel_source: station has no fuel` (no "insufficient credits") → no record.
  - `TestRefuel_RichAgentDoesNotFile` — same error, credits 12000 after GetStatus → no record.
  - `TestRefuel_UndockedDoesNotFile`.
  - `TestRefuel_SuccessDoesNotFile`.
  - `TestRefuel_FilerErrorIsSwallowed` — filer returns error → Refuel still returns the original refuel error (not the filer's).
- [ ] **Step 2:** `go test ./pkg/worker -run TestRefuel_` → FAIL.
- [ ] **Step 3:** Implement; wire in `cmd/worker/main.go` right after the client is created and before `NewWorkerDispatch` / autopilot use it, so both refuel sites in `pkg/worker/autopilot.go` go through the decorator.
- [ ] **Step 4:** `go build ./... && go test ./pkg/worker ./cmd/worker && golangci-lint run ./pkg/worker/... ./cmd/worker/...` → PASS, clean.
- [ ] **Step 5:** Commit: "worker: file a credit request when a station refuel fails for lack of credits".

### Task 3: Banker verb `pay_credit_requests`

**Files:**
- Create: `pkg/worker/pay_credit_requests.go`, `pkg/worker/pay_credit_requests_test.go`
- Modify: `pkg/worker/dispatch.go` (case + `supported` vocabulary + a `CreditQueue`/`CreditHistory` field set from `cmd/worker/main.go`), `pkg/worker/schedule.go` only if verbs must be listed for phase/wide rules (follow `capture_base`'s precedent).

**Interfaces:**
- Consumes: Task 1 queue/history.
- Produces:
  - `type GiftSender interface { SendGift(ctx context.Context, payload map[string]any) error }` (satisfied by `game.GameClient`).
  - `type PayDeps struct { Queue *creditreq.Queue; History *creditreq.History; Sender GiftSender; OwnCredits func() float64; Usernames func(agentID string) string; Now func() time.Time; Out io.Writer }`
  - `func PayCreditRequests(ctx context.Context, d PayDeps) (paid, held int, err error)`
  - Rules, in order per pending record (oldest `RequestedAt` first): own credits < 1,000,000 → stop the run, pay nothing, log once; username = record's or `Usernames(agentID)`, empty → held `no username`; `PaidSince(history, agent, now-24h) ≥ 1` → held `paid <ts> already`; `TotalPaidSince(history, UTC midnight) + 20000 > 1,000,000` → held `daily cap`; else `SendGift({"recipient": username, "credits": 20000})` → on success append history `paid` FIRST, then `Remove` the record; on error increment `Attempts`, append `send_failed`, and at 3 → held `send failed: <err>`. Every hold appends a `held` entry.
  - Dispatch `case "pay_credit_requests"` builds `PayDeps` from `d.Client` (OwnCredits = state `Player.Credits`), usernames from `d.Assets` agents table (same query as `cmd/tools/banker` `loadUsernames`), prints `pay_credit_requests: paid N, held M`.

- [ ] **Step 1: Failing tests** (fake sender records payloads; temp queue/history):
  - `TestPay_PaysPending` — one pending record → one gift `{"recipient":"Josephus Miller","credits":20000}`, record removed, history has `paid` 20000.
  - `TestPay_24hRuleHolds` — history paid 2h ago → held, no gift.
  - `TestPay_DailyCapHolds` — history 50×20000 today → held `daily cap`.
  - `TestPay_LowBankerPaysNothing` — OwnCredits 900000 → 0 gifts, record still pending.
  - `TestPay_UnknownUsernameHolds`.
  - `TestPay_ThreeSendFailuresHold` — sender errors three runs → held on the third, 3 `send_failed` entries.
  - `TestPay_HistoryBeforeTransition` — history Append succeeds, queue path made read-only before Remove → a second run does NOT gift again (24h rule sees the paid entry).
  - `TestPay_OldestFirst`.
- [ ] **Step 2:** `go test ./pkg/worker -run TestPay_` → FAIL.
- [ ] **Step 3:** Implement + dispatch wiring.
- [ ] **Step 4:** `go build ./... && go test ./pkg/worker && golangci-lint run ./pkg/worker/...` → PASS, clean.
- [ ] **Step 5:** Commit: "worker: pay_credit_requests — banker pays queued credit requests".

### Task 4: fleet-watch alerts

**Files:**
- Modify: `cmd/tools/fleet-watch/watch.go`, `cmd/tools/fleet-watch/main.go`, `cmd/tools/fleet-watch/watch_test.go`

**Interfaces:**
- Consumes: Task 1.
- Produces: `KindCredit = "credit"`; `func CreditAlerts(open []creditreq.Record, hist []creditreq.Entry, now time.Time) []Alert` — one alert per held record (`<agent> credit request held: <reason>`), per pending older than 30 min (`<agent> credit request pending <N>m — is the banker paying?`), per agent with ≥2 `paid` in 7 days (`<agent> paid N times in 7 days`). `Notifiable(KindCredit)` = true. main.go reads the two files each cycle (flag `--credit-requests`, same default) and appends the alerts.

- [ ] **Step 1: Failing test** `TestCreditAlerts` — table: held → 1 alert; pending 10 min → none; pending 45 min → 1; two paid in 3 days → 1; one paid → none.
- [ ] **Step 2:** `go test ./cmd/tools/fleet-watch -run TestCreditAlerts` → FAIL.
- [ ] **Step 3:** Implement and wire.
- [ ] **Step 4:** `go test ./cmd/tools/fleet-watch && golangci-lint run ./cmd/tools/fleet-watch/...` → PASS.
- [ ] **Step 5:** Commit: "fleet-watch: alert on held, stale and repeat credit requests".

### Task 5: Go live

**Files:**
- Modify: `data/agents/the_banker/schedule.json` (runtime; not committed), `data/overmind/roles.yaml` (banker role seed), `docs/COLD_START.md` only if fleet-watch's launch line needs the new flag.

- [ ] **Step 1:** `go build -o bin/worker ./cmd/worker && go build -o bin/fleet-watch ./cmd/tools/fleet-watch`.
- [ ] **Step 2:** Add `{"frequency":"ten_minutely","command":"pay_credit_requests"}` (next free id) to the_banker's schedule.json and the banker role in roles.yaml.
- [ ] **Step 3:** Roll the binary out per `docs/memory/reference_rebuild_before_restart.md`: banker fleet first, then one fleet at a time (SIGUSR1 drain; verify by worker start time vs `bin/worker` mtime). Never restart a disconnected worker. Restart fleet-watch.
- [ ] **Step 4: Verify:** the banker log shows `pay_credit_requests: paid 0, held 0` within 10 min; then `go run ./cmd/debug/...` is NOT used — instead wait for (or seed by hand with `creditreq` JSON) one real pending record for a 0-credit agent and confirm a `paid` history line and the agent's next `Refueled at station`. Expected: all true.
- [ ] **Step 5:** Commit roles.yaml (+ COLD_START if changed): "credit requests live: banker pays every 10 min".
