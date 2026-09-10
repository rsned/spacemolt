---
name: reference_rate_limit_buckets_and_escalation
description: "Server-confirmed rate-limit model — five named buckets in details.limit, blocks escalate 2min→30min, and the server exposes NO view of what tripped a block, so our own logs are the record"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2d7bbf54-7b88-4de5-ba93-9477eb1cb891
  modified: 2026-09-09T22:07:58.329Z
---

**Source: SpaceMolt Bug Bot, 2026-09-09**, answering "what tripped our IP block".

## The model

- An IP block is earned by **piling up ordinary `rate_limited` errors** across
  the fleet's shared IP. The block is the consequence, not the event.
- Each `rate_limited` error names its bucket in **`details.limit`**:
  **`game_query` · `game_mutation` · `public_api` · `session_auth` ·
  `connection`** — usually with `limit_per_min` and `current`, plus a `hint` in
  `message`.
- **`retry_after` placement varies by transport**: HTTP puts it *outside*
  `details` — at the body root OR inside the error object, depending on endpoint
  — alongside a `Retry-After` header; WebSocket puts it inside the error frame.
  **MCP tool errors carry ONLY the message text**, so log the whole string there.
- Once blocked: WebSocket returns `ip_timed_out` with the countdown in the text;
  HTTP returns **429 with `limit: "ip_timeout"`**.

## ⭐🔴 The two operational rules

1. **There is nothing to learn from inside a block, and nothing to gain from
   retrying.** Every command returns the same countdown. Wait it out.
2. **Resuming at the same pace after it lifts makes the next block LONGER —
   they escalate from 2 minutes up to 30** (per the API docs). This is the
   justification for the staged thaw and for keeping load reduced afterwards:
   coming back at full pace is how a 2-minute block becomes a 30-minute one.
   See [[reference_ip_block_20260909_freeze_and_staged_thaw]].

## ⭐ The server keeps NO record — ours is the only one

- No endpoint describes what tripped a block.
- **`get_action_log` records only actions that SUCCEEDED** (jumps, mining,
  trades, logins). It never records rate-limit hits or request counts, so it can
  say what an agent did but never why the IP was blocked.
- Therefore: **a tally of our own logged `rate_limited` errors, by bucket, IS
  the incident record.** Nothing else can be reconstructed after the fact.

## What we built (2026-09-09, UNDEPLOYED)

`pkg/game/rate_limit_bucket.go` — `rateLimitBucketFrom()` parses the precursor
out of any error payload and `parseErrorState` logs it as a greppable line:

```
rate_limited bucket=game_query limit_per_min=120 current=131 retry_after=7s
```

Tally an incident with:
`grep -o 'rate_limited bucket=[a-z_]*' data/overmind/*-overmind.log | sort | uniq -c | sort -rn`

Design points that matter:
- **It logs via a new always-on `Client.eventLogger`, NOT `debugLogger`** —
  `SetDebugLogging(false)` points debugLogger at `io.Discard`, so a precursor
  logged there would exist only when someone already had `--debug` on, i.e.
  never during the incident you did not predict. There is a regression test for
  exactly this (`TestRateLimitPrecursorLoggedWithDebugOff`).
- **`ip_timed_out` is deliberately EXCLUDED** from the tally: it is the block
  itself, would double-count the incident, and names no bucket.
- A `rate_limited` with no nameable bucket still counts, as `unknown` — dropping
  it would under-count the tally.
- Bucket is recovered from message TEXT when no structured `details` arrive,
  which is the only way MCP-transport limits become visible.

Distinct from `recordRateLimitBlock` (`login_gate.go`), which handles
connect-time blocks and publishes them to the host-wide reconnect gate; and from
`rateLimitDetail` (`rate_limit_detail.go`), which is HTTP-429-only and reads
`retry_after` but not the bucket.

**Needs a fleet roll to take effect** — deliberately NOT deployed on the night of
the block, since a mass relogin is exactly what escalates the next one.

See [[reference_login_rate_limits]] · [[reference_sigstop_preserves_game_sessions]]

## ⭐🔴 Correction 2026-09-09: buckets are named in PROSE, and 170 is not too many

**Two corrections to the section above, both from the operator.**

**1. The message names the bucket in prose, not as an identifier.** Observed
forms: **"OpenAPI spec fetches"**, **"catalog dump downloads"**, **"public API
requests"**. The snake_case list (`game_query`, `game_mutation`, …) is what
`details.limit` carries *when it is present*; the free-text name is different
wording for the same idea and matches no identifier we could have guessed.

So: **log the message VERBATIM and do not normalise it.** Any parser is a filter
that silently drops the cases we did not anticipate, and an MCP tool error
carries the text and nothing else. Implemented in `fc6f5d41` — the log line
keeps `msg="…"` (bounded 300 chars) beside the structured bucket.

Note those three examples are all **public-API/data-plane** names, not game
commands. If our block came from that family, the culprit would be a
catalog/OpenAPI fetcher, **not the workers at all** — checked 09-09 and no
scraper or cron fetcher was running, so this is unconfirmed, but it is the first
thing to check against the next captured message.

**2. ⭐ Other operators run ~1000 agents on ONE IP with no blocks.** Our 170 are
therefore not inherently too many, and **fleet size is not the cause**. My
"steady-state volume from 170 workers" conclusion was the residue of an
elimination, reported as an answer — it was wrong. Something *structural* is
over-issuing, and `idle_ticks` is a mitigation that buys headroom, not a fix.

**What finds it: `send_tally`** (`pkg/game/send_tally.go`, `fc6f5d41`). Every
worker logs its outbound commands BY TYPE every 5 minutes
(`game.SleepSendTally`), ordered by volume:

```
send_tally total=142 get_nearby=61 get_status=40 dock=21 ...
```

The pre-existing `messagesSent` counter says how MUCH we sent but never WHAT, so
one wasteful command repeated by every worker looked exactly like healthy
traffic. Fleet-wide tally:

```
grep -ho 'send_tally .*' data/overmind/*-overmind.log | tr ' ' '\n' \
  | grep '=' | awk -F= '{a[$1]+=$2} END {for (k in a) print a[k], k}' | sort -rn
```

**The bucket names the METER; the tally names the COMMAND that filled it.** Both
are needed — that is why they are two mechanisms and not one.
