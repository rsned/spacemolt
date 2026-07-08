# API Struct / Command Drift Audit — Findings

**Date:** 2026-07-08
**Branch:** `feat/api-drift-audit`
**Server target:** v0.473.0 (`data/game-api/latest/get_commands.json` + `server_docs/openapi.json`, dated 2026-07-06/07)
**Design:** `docs/superpowers/specs/2026-07-08-api-drift-audit-design.md`
**Plan:** `docs/superpowers/plans/2026-07-08-api-drift-audit.md`

## Why

`BuiltForAPIVersion` was bumped `v0.397.0` → `v0.473.0` (`af8e099`) to match the catalog
snapshot, but with no audit of response structs or command signatures — so the client
*asserted* compatibility across ~75 minor versions nobody had checked, silencing the
`CheckVersion` drift signal. This audit reconciles the claim and makes it honest.

## Method

A throwaway in-package Go harness reflected over `pkg/game.actionResponseTypes` (the
204-entry command→response-struct map) and diffed three layers against the authoritative
v0.473.0 snapshot. The harness resolved openapi `*Response` schemas through
`$ref`/`allOf`/`oneOf` (26 of 201 schemas wrap fields in `oneOf`; a naive reader falsely
flagged them as 100% drift). Live sample JSON in `data/game-api/latest/` was treated as
ground truth; openapi as a broad but **incomplete superset** (it documents fields the
server *may* send, not what any given response *does* send). The harness was deleted after
use; its command-coverage half was promoted to a permanent regression test.

**Key interpretive lenses (why raw diff counts overstate drift):**
- `encoding/json` silently ignores unknown server fields, so a `schema-only` field (server
  documents it, client struct lacks it) is **not a break** — it is data the client
  intentionally does not model.
- `go-only=[action]` is a systematic false positive: most client structs carry an `action`
  echo field the openapi schemas omit.
- `go-only=[command,pending]` on mutation responses are client-side synthetic
  request-tracking fields, not server data.

## Layer 1 — Command coverage — RECONCILED + GUARDED

214 server commands vs the client's `actionResponseTypes`. Result: 5 handled-but-unmapped
commands registered, 30 genuinely-unsupported commands moved to a justified ignore-list,
and a permanent guardrail added.

**New guardrail:** `pkg/game.TestServerCommandsCoveredByClient` fails when a
`get_commands.json` command is neither typed in `actionResponseTypes` nor in
`ignoredCommands`. This closes the reverse-direction gap the existing
`TestLoadFromOpenAPIContainsAllHardcoded` never covered, so command-coverage drift now
fails the build instead of accumulating silently.

### Registered (handled by the client; response struct already existed)
| command | response struct | note |
|---|---|---|
| `get_achievements` | `GetAchievementsResponse` | issued via play_as passthrough |
| `list_passengers` | `ListPassengersResponse` | passenger feature |
| `load_passenger` | `LoadPassengerResponse` | passenger feature |
| `list_station_passengers` | `ListStationPassengersResponse` | passenger feature |
| `unload_passenger` | `UnloadPassengerResponse` (new, minimal) | see Layer 3 break #1 |

### Ignore-list dispositions (each carries this justification in code)
| commands | reason |
|---|---|
| `v2_get_player/ship/cargo/skills/missions/queue`, `get_state`, `get_location`, `storage` | v2 API; client is on v1 (`project_v2_api_migration`). `get_location` consumed generically; `get_poi→get_location` migration on hold. |
| `subscribe_market`, `unsubscribe_market`, `subscribe_observation`, `unsubscribe_observation` | streaming subscriptions; no client consumer |
| `faction_garages`, `hunt`, `build_outpost`, `buy_ship_license`, `place_ship_buy_order`, `cancel_ship_buy_order`, `view_ship_buy_orders`, `sell_ship_to_order`, `prepay_tax`, `faction_prepay_tax`, `faction_scan_poi`, `get_faction_achievements`, `get_faction_tax_estimate`, `get_notification_settings`, `mute_notifications`, `unmute_notifications`, `station` | not implemented |

### Reverse direction (25 `actionResponseTypes` keys that are not server commands) — no code change
These are harmless: server-push **event types** (`arrived`, `jumped`, `battle_alert`,
`mobile_capital_transit`, `raid_status`, `retreat`, `session`, `agentlogs`) and
response-`action`-value keys for multiplexed commands (`get_listings`, `get_recipes`,
`get_ships`, `sell_all`, `browse_for_sale`, `owned`, `types`, `list`, `queue`, `job_list`,
`set_anonymous`, `gift_ship`, `attack_base`, `search_changelog`, `buy_ship`). Extra map
keys cost nothing (they are only consulted when the server emits that action). `salvage_wreck`
is confirmed retired (already removed from `actionspace` on 2026-07-08) but its harmless map
key was left in place.

### Known limitation
`actionResponseTypes` powers "unknown field detection" only when a response carries an
`action` key; `CheckOKResponseFields` returns early otherwise. All 5 newly-registered
commands return **action-less** payloads, so registering them satisfies coverage but does
**not** enable runtime field-drift detection for them. This is a pre-existing, systemic
limitation of that mechanism, not a regression.

## Layer 2 — Payload shape — AUDITED, NO CLEAR BREAKS

Server `format`-example payload keys vs client-built payload keys, both directions.
- **Server keys the client omits** are optional/pagination params (`page`, `page_size`,
  `limit`, `category`, `search`, `sort_by`) or belong to pass-through payload methods
  whose callers build the keys. Spot-checked the highest-risk case — `create_buy_order` /
  `create_sell_order` — and confirmed all callers (`pkg/worker/haul.go`, `auto-trader`,
  `bulk-*-order`, `play_as`) use the correct server key `price_each`.
- **Client keys the server example omits** were only two, both **documented optional
  variants** the single-line `format` example didn't show: `deploy_drone.all` ("Pass
  all: true to deploy every in-bay drone") and `loot_wreck.item_id`/`quantity`/`module_id`
  ("To loot a specific cargo item: include item_id and optional quantity").

No payload-shape fix required.

## Layer 3 — Response-struct fields — 1 break fixed, 1 latent break fixed (Layer-1 spillover), rest documented

The harness found 71 structs with field deltas and 29 UNVERIFIED (no schema / opaque
object). Per the breaks-only scope, only client-read fields **absent from a live sample**
count as fixable breaks. Four structs had live samples confirming absent client fields;
all others are openapi-only signal (incomplete superset) and are **documented as
unverified**, not touched.

### Break #1 (fixed) — `unload_passenger` fare field
The server field is **`fare_collected`** (openapi required integer), but the client used
`fare_paid` in three live places: `UnloadPassengerResponse`'s json tag, `client.go`'s
passenger-response content detection (keyed on `fare_paid`, so it **never matched** a real
unload reply), and `play_as`'s formatter (printed "0 cr" for delivered passengers).
Fixed all three + test fixtures (`fe2dcdb`). Surfaced while creating the struct in Layer 1.

### Break #2 (fixed) — `view_orders` faction scope
`ViewOrdersResponse.FactionOrders` does not exist on the wire at v0.473.0. `view_orders`
now scopes to `personal` (default) or `faction` via a request `scope` param, returning
faction orders in the same `orders` field. The faction collector requested the default
(personal) scope and read the nonexistent `FactionOrders`, so **faction order collection
silently wrote zero rows to the KB**. Fixed: request `scope:"faction"` and read
`resp.Orders` (`c376ba7`, `pkg/faction/collector.go` + `parse_market.go` + test).

### Documented, not changed (per breaks-only scope)
- `GetStatusResponse` (`current_tick`, `nearby`, `poi`, `system`) and `GetBaseResponse`
  (`market`, `poi`, `resources`): absent from live sample **and** openapi, but the structs
  are **never unmarshaled** — real state flows through separate raw-payload parsers. Dead
  DTO fields; harmless. Left in place (removal is out-of-scope cleanup).
- `ViewStorageResponse.Credits`: consumers exist (`storage_capture.go`, `daily-summary`),
  but no server command lets personal storage hold credits, so an always-zero value may be
  accurate rather than drift. Ambiguous; left unchanged.
- The remaining ~67 field-delta structs and 29 UNVERIFIED structs: openapi-only signal or
  no authority. Absence of a field from the incomplete openapi superset is **not**
  evidence of drift, and no live sample exists to confirm. High-traffic structs
  (`TravelResponse`, `JumpResponse`) show large openapi deltas but the running fleet
  proves the client reads them fine — openapi simply omits the fields. Not verifiable
  without populated live samples; left unchanged and flagged here.

## Honest verdict on `BuiltForAPIVersion = v0.473.0`

- **Command coverage: VERIFIED and guarded.** Every server command is accounted for
  (typed or justified-ignored), enforced by `TestServerCommandsCoveredByClient`.
- **Payload shapes: VERIFIED** for statically-analyzable commands; no drift.
- **Response structs: PARTIALLY verified.** Fields the client *reads* are verified for
  every response with a live sample; two genuine breaks were found and fixed there.
  Structs without a live sample remain **unverified** — openapi is an incomplete superset,
  so its deltas are not reliable drift signal. These are itemized above.

The version claim is now **honest**: command compatibility is truly verified and
drift-guarded, and the response-struct verification is explicitly scoped to
sample-backed structs, with the unverified remainder named rather than silently asserted.
A caveat pointing here was added to the `BuiltForAPIVersion` doc comment in
`pkg/version/checker.go`.

### Follow-up (optional, out of scope)
Capturing populated live samples for the high-traffic unverified structs (travel/jump/
combat/facility) would let a future round verify them against ground truth instead of the
openapi superset.
