# Executor B — live mechanics findings (Task 0)

Verified 2026-07-10 against server **0.486.4**, live probes as craftsman-2 /
craftsman-3 (play_as, --debug-full-payload). Raw JSON below is copied from
captured responses.

## Decisions for implementation

1. **send_gift payload:** `{"recipient": "<in-game USERNAME>", "item_id": "...",
   "quantity": N}`. Recipient is the username (`Artisan 'Ace' Anderson`), NOT
   an agent id → the plan runner (Task 5) and handoff records (Tasks 1/10)
   matter: params and `handoff.Record.Recipient` carry AGENT IDS (or `self`);
   the worker-side gift verbs resolve `agent_id → username` via
   `data/agents/<id>/credentials.json` at gift time (controller decision,
   post-Task-4; keeps the plan runner free of credential reads).
2. **Gifts are CARGO-only.** Gifting from storage fails
   (`insufficient_cargo: You only have 0 x pressure_seal in cargo.`).
   `--source=storage` is NOT live as of 0.486.4 — keep the batched
   withdraw→gift loop (Task 10), revisit when the patch lands.
3. **Gift landing: recipient's STORAGE at the SENDER'S station, immediately,
   regardless of where the recipient is.** Verified cross-system: craftsman-3
   (docked at confederacy_central_command, Sol) received `pressure_seal x1`
   in its storage at grand_exchange_station (Haven). Early-gift with no
   courier synchronization is fully validated.
4. **craft `quantity` = OUTPUT UNITS.** Probe: `quantity: 6` on
   verdigris_smelting (produces 3 copper_piping/run) → `"runs": 2`, inputs
   ×2. The server does the ceil-divide — Task 8's verb passes NUM_OUTPUTS
   directly as `quantity`; `output_per_run` is only needed for the
   storage-recompute step.
5. **Crafting requires being docked at the facility's station.** A remote
   `facility_id` errors `no_facility` ("No active facility with id '…' at
   this station … or omit facility_id to auto-route"). Auto-route exists
   (omit facility_id → server picks a local facility / workshop).
6. **The `no_facility` error names the nearest public facility**, e.g.
   "'Assemble Dark Energy Cell' is made in a Dark Energy Containment
   Reactor… Nearest public one: The Experiment Research Station in The
   Experiment (18 jump(s) away)…" — surface this verbatim in park details.
7. **Hand-craft venue** is `workshop:<owner_id>:<station>` (venue_type
   `workshop`); it consumes materials **from storage** ("You don't currently
   have all the materials in storage") and "advances only while you're
   docked here and pauses if you undock" — the craft verb must stay docked
   until completion.
8. **deliver_to ∈ {storage, faction}** — routes output at the SAME station
   only. It does NOT deliver cross-station: synthetic transport nodes stay
   (spec amendment stands).
9. **`view_storage` accepts `{"station_id": "<base>"}`** and returns the
   caller's OWN storage at that remote base (verified cross-system) — verbs'
   recompute-remaining can check destination storage without traveling.
10. **Mutations are tick-paced.** send_gift/withdraw ack `pending: true`
    then resolve as `action_result`/`action_error` on the next tick
    (~10s). Verbs must use the Submit/terminal-response paths, never assume
    synchronous completion.

## CraftDryRunResult — authoritative field set (Task 8)

```json
{
  "action": "craft",
  "cost": {"inputs": [{"item_id": "verdigris_curd", "name": "Verdigris Curd", "quantity": 8}]},
  "credits_total": 0,
  "dry_run": true,
  "effective_time_per_run": 0.2272727272727273,
  "est_completion_tick": 1314486,
  "facility_id": "workshop:c5a5c5a2e8263ff146b423000ea7c295:grand_exchange_station",
  "have_credits": true,
  "have_inputs": false,
  "message": "Quote only — nothing queued. …",
  "mode": "craft",
  "produces": [{"item_id": "copper_piping", "name": "Copper Piping", "quantity": 3}],
  "quantity": 6,
  "recipe": "Verdigris Smelting",
  "runs": 2,
  "venue": "Station Workshop",
  "venue_type": "workshop"
}
```

`have_inputs` / `have_credits` are preflight gold: the craft verb checks both
before queueing; `credits_total` is the authoritative fee for the budget gate.

## Raw probe evidence

### send_gift from storage (fails — cargo-only)

```json
{"code":"insufficient_cargo","command":"send_gift","message":"insufficient_cargo: You only have 0 x pressure_seal in cargo.","tick":1314472}
```

### withdraw then gift (succeeds)

```json
{"command":"withdraw_items","result":{"action":"withdraw_items","cargo_space":398,"cargo_total":2,"item_id":"pressure_seal","quantity":2,"storage_remaining":2329},"tick":1314473}
{"command":"send_gift","result":{"action":"send_gift","base_id":"grand_exchange_station","cargo_remaining":1,"item_id":"pressure_seal","quantity":1,"recipient":"Artisan 'Ace' Anderson"},"tick":1314474}
```

### Recipient-side confirmation (craftsman-3, docked in Sol)

```json
{"base_id":"grand_exchange_station","hint":"12,128 items in storage at confederacy_central_command, grand_exchange_station","items":[{"item_id":"pressure_seal","name":"Pressure Seal","quantity":1,"size":1}],"ships":[]}
```

### Remote facility_id (fails — must be docked there)

```json
{"code":"no_facility","message":"No active facility with id '62cfdf6c4dae01059852c6810e997aff' at this station. Use facility action=list to see facilities here, or omit facility_id to auto-route."}
```

### no_facility with nearest-site hint

```json
{"code":"no_facility","message":"'Assemble Dark Energy Cell' is made in a Dark Energy Containment Reactor, and no facility here can make it. Nearest public one: The Experiment Research Station in The Experiment (18 jump(s) away) — travel there to queue it, or buy 'Assemble Dark Energy Cell' on the exchange. To make it here, build a Dark Energy Containment Reactor (facility action=build)."}
```
