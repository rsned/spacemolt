# Mission Abandon-Reason Catalog

Date: 2026-07-17
Status: LIVING DOCUMENT — extend whenever a new abandon path is added.

Every `missionAbandon` call passes a stable reason slug, recorded in
`mission_results.reason` (market.db; `ensureColumn` migration backfills the
column, historic rows read as `''`). Query the live distribution with:

```sql
SELECT reason, COUNT(*), MIN(finished_at), MAX(finished_at)
FROM mission_results WHERE outcome='abandoned' GROUP BY reason;
```

Abandoning costs **zero credits** (measured live 2026-07-17, wallet
310,006 → 310,006) — the cost of an abandon is the wasted accept slot, any
already-spent item/fuel cost, and possibly reputation (unmeasured).

## Reasons

| Slug | Trigger (code path) | Solvable? | Fix path / status |
|---|---|---|---|
| `buy_failed` | Accept-time acquisition: storage withdraw left a remainder and the station `buy` errored (mission.go accept loop) | **Mostly solved** | The pre-accept availability gate (`176f18e`, storage + local sell-depth check) eliminated the churn era (8 soak rows, all pre-gate). Residual = market moved between gate and buy. Further: re-check gate once and retry, or hold the mission and buy at the next station visit. |
| `stronghold_destination` | Resume: a held mission's destination system is a pirate stronghold (missionResume) | **Per-agent solvable, later** | Only reachable by pre-guard accepts or KB updates mid-hold — the accept-time stronghold gate already blocks new ones. Permanently solvable per agent via the smuggling-chain pirate-rep onboarding (treasure_cache, 2 chains → stronghold access). Until then: correct to abandon. |
| `cargo_lost` | Resume: held deliverable, cargo not aboard, target-base storage doesn't cover the remainder (missionResume) | **Solvable (v2)** | Causes: death, jettison, orphaned provided-cargo. Fix: re-buy the missing units when net stays positive; needs the availability-gate check against the CURRENT station plus budget guard. Until then: abandon is the safe floor. |
| `staged_non_nav` | (Planned — exploration executor) a staged mission appended an objective outside the handled vocabulary (e.g. deliver/kill leg appearing after a nav leg) | **Shrinks over time** | Each new objective-type handler (delivery-leg reuse, mining, combat) moves missions from this bucket to runnable. The slug tells us exactly which vocabulary to teach next — query the titles carrying it. |

## Non-abandons (for contrast)

- **Pre-accept skips** (`not a plain deliver mission`, `net below floor`,
  `stronghold route`, `no reference ask`, availability gate, …) are FREE —
  never accepted, nothing recorded in mission_results. The skip-reason log
  dedup (`shouldLogSkip`) is their observability.
- **Held/retry** (transit or dock failure on resume) is not an abandon —
  the mission stays active and retries next pass.
- **`expired` outcome** — not yet emitted (v1 gap, queued): a held mission
  past its expiry should record `expired`, not be silently re-chased.

## Maintenance rule

Adding a `missionAbandon` call without a new or existing slug from this table
is a review defect. When a slug's live count grows, that's the signal its fix
path has become worth building — check the query above during fleet reviews.
