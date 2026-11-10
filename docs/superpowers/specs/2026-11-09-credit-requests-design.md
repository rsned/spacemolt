# Credit requests: agents ask the banker before they strand — design

Date: 2026-11-09. Status: main points approved by the operator in conversation.

## Why

On 2026-11-09, 16 seconded unlock agents sat docked with **0 credits** and a near-empty
tank for most of a day ("Station refuel failed: no_fuel_source: No fuel cells in cargo and
insufficient credits for station refueling"; "NOT DEPARTING: route needs ~84 fuel, 5
available"). Nobody knew until the operator asked how the unlock was going. the_banker
held 126M the whole time. The `banker` tool (cmd/tools/banker) tops agents up, but only
when an operator runs it. The operator wants agents to be **proactive**: the moment one
cannot pay for fuel, it asks, and money arrives within minutes, not days.

Operator choice (2026-11-09): **ask-on-failure** (option 2), not a blanket scheduled
sweep and not alert-only.

## Goals

1. When a worker's station refuel fails for lack of money, it files a **credit request**
   on a shared cross-overmind queue.
2. the_banker's worker pays pending requests from its own live session on a schedule:
   `send_gift <username> credits 20000`.
3. Guard rails: at most **one payment per agent per 24 h**, **1,000,000 per UTC day**
   fleet-wide; a request beyond either is **held** for the operator, not paid.
4. Every payment and every hold is recorded (history JSONL) and visible as a fleet-watch
   warning.

## Non-goals

- Agents stranded **undocked** with an empty tank (credits do not help; that is the
  rescue queue's job — e.g. trucker-00..02 at Dim Fortune).
- Replacing the existing faction-treasury self-rescue (`treasuryRescue`, needs
  `manage_treasury`) or the manual `banker` tool; both stay.
- Funding non-fuel spending (bounties, purchases).

## Anti-goals

- A loop that pays an agent that keeps burning money: the 24 h rule + hold covers it.
- Extra logins or play_as sessions: payment runs inside the banker worker's existing
  connection (the clerk login gate is 60/min and must not be raised).
- Failing a worker's pass because the queue file is busy or unwritable: filing is
  best-effort and logged.

## Design

### Trigger (worker side)

Fire when a station refuel returns an error containing `no_fuel_source` (the server's
"No fuel cells in cargo and insufficient credits for station refueling") **and** the
agent is docked **and** its credits are below **5,000**. The two refuel-failure sites are
`pkg/worker/autopilot.go` (~:135 and ~:440, both print "Station refuel failed"); the
builder routes both (and `refuel_sync.go` if it reaches the same error) through one
helper. The worker needs its agent id and the queue; follow how `RescueQueue` reaches the
dispatch (`pkg/worker/dispatch.go:49`) — a narrow interface injected from
`cmd/worker/main.go`, nil = disabled.

One **pending** request per agent: filing again while one is pending is a no-op.

### Queue

New package `pkg/creditreq`, modelled on `pkg/rescue/queue.go`: a flock-guarded JSON
file, **`data/overmind/credit-requests.json`**. Record:
`agent_id, username, station (base id), system, credits, fuel, max_fuel, reason (server
error text), status, requested_at, paid_at, amount, held_reason`.
Lifecycle: `pending → paid | held`. `held` waits for the operator (cleared by editing the
file or a small tool flag, builder's call).

### Payment (banker side)

New worker verb **`pay_credit_requests`** in the_banker's schedule, frequency
**`ten_minutely`**. For each pending record, oldest first:

- paid to this agent in the last 24 h → `held` ("paid <ts> already").
- today's total + 20,000 > 1,000,000 → `held` ("daily cap").
- otherwise `send_gift "<username>" credits 20000`; on success `paid` (amount, paid_at);
  on a send error leave it `pending` and log (retry next run).

Username comes from the record (filled at filing time from the agents table / worker
credentials). Every transition appends a line to
`data/overmind/credit-requests-history.jsonl`. The 24 h and daily-cap checks read that
history.

### Visibility

fleet-watch (or the overmind status warnings, builder's call) reports: any `held` record;
any `pending` record older than 30 min (banker not paying); any agent paid ≥2 times in 7
days.

## Values

- Amount 20,000; threshold credits < 5,000; one payment / agent / 24 h; 1,000,000 / UTC day;
  banker run every 10 min (`ten_minutely`); stale-pending warning 30 min; repeat-payer
  warning 2 payments / 7 days.

## Left to the builder

Package and file layout beyond the names above, the exact injection seam, how `held` is
cleared, where the warnings render, and tests (TDD; the queue gets the same lock/transition
tests as `pkg/rescue`).

## Builder-check rulings (assistant's calls, 2026-11-09)

1. **Seam:** a client decorator, not parameter threading. `cmd/worker/main.go` wraps the
   worker's `game.GameClient` in a `creditRequestingClient` (pkg/worker) whose `Refuel`
   calls through and, on the trigger below, files the request. The free functions in
   autopilot.go keep their signatures. Nil queue = no wrap.
2. **Username:** agent id ≠ in-game username. The worker reads `username` from its own
   `data/agents/<id>/credentials.json` at startup and passes it to the decorator, which
   writes it into the record. No username → still file; the banker resolves it from the
   assets `agents` table (as `cmd/tools/banker` does) and holds the record if that fails.
3. **Banker schedule:** add `ten_minutely pay_credit_requests` to the_banker's
   `data/agents/the_banker/schedule.json` directly (the worker reloads it on mtime), the
   same way CJ's captures were added; also add it to roles.yaml for the banker role so a
   fresh seed has it.
4. **Banker balance / send errors:** if the banker's own credits are below 1,000,000 the
   run pays nothing, leaves records pending and logs one warning. A record whose gift
   fails 3 times becomes `held` ("send failed: <error>").
5. **Trigger:** require the refuel error to contain **both** `no_fuel_source` and
   `insufficient credits`, the agent docked, and credits < 5,000 after a fresh `GetStatus`.
6. **Records:** the queue holds at most one open record per agent (`pending` or `held`); an
   open record blocks new filing. `paid` records leave the queue and live only in the
   history JSONL, so the agent can file again after it (the 24 h rule then decides).
7. **No expiry:** a pending record is paid even if hours old (cost ≤ 20k); the 30-min
   stale-pending warning is the signal that something is wrong.
