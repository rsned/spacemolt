---
name: project_passenger_feature
description: "Passenger-carrying commands + play_as formatters, and the RawCommand blocking-behavior change they required"
metadata: 
  node_type: memory
  type: project
  originSessionId: 04732b6c-502d-4abf-833c-75d96cdc3a76
---

Server added passenger carrying (ferry paying citizens between stations): 4 commands + 3 utility-slot cabin modules (economy/business/first; `passenger_*_berths` capacity fields). Built play_as styled formatters 2026-06-07 (`cmd/tools/play_as/passenger_format.go`).

Commands & params (verified against live server):
- `list_passengers` (aboard) — payload key `passengers`; berth gauges `economy_berths`/`business_berths`/`first_berths` ("used/avail").
- `list_station_passengers [station]` — payload key `waiting`; per-passenger `name`/`class`/`citizenship`/`citizen_id`/`destination_name`. Grouped by destination (alpha) → class (economy→business→first).
- `load_passenger <destination>` — takes a **destination station** (loads ALL waiting passengers bound there), NOT a passenger id. Deferred mutation.
- `unload_passenger <name|citizen_id>` — takes a passenger name/id. Deferred mutation.

Two plumbing fixes were needed (not just formatters):
1. **storeRawJSON keying** ([[project_passenger_feature]] in `pkg/game/client.go`): these `ok` responses carry no `action` field, so they were only cached under `_last`. Added content-based detection (keys `waiting`/`passengers`/`total_fare`/`fare_paid`) so play_as `lookupRawJSON(command)` finds them.
2. **RawCommand now BLOCKS on the terminal** (`pkg/game/client_commands.go`): changed from fire-and-forget `send` → `Submit(WithTerminator(terminateOnActionOrOK))`. This resolves immediately on a synchronous non-pending `ok` (queries) but waits through `pending:true` for the `action_result` terminal that deferred mutations deliver next tick. **This affects ALL generic-passthrough commands** (view_orders, catalog, refuel, repair_module, use_item, name_ship, etc.), which previously only rendered on a 2nd call or showed the bare pending ack. Net win, but a broad behavior change — autopilot `use_item` now waits for its terminal.

play_as needs explicit case handlers (not the generic arg1/arg2 passthrough) for commands with named params — added for all 4 passenger commands. Server echoes `request_id` on responses globally (confirmed: load_passenger reply echoed the sent id), which is what makes the Submit ack/terminal correlation work.

**dock passenger_arrivals (2026-06-07):** docking at a passenger's destination auto-delivers them + collects fare. Added `PassengerArrivals`/`DeliveredPassenger` to `serverapi.DockResponse` and surfaced deliveries/fare/reputation in play_as `formatDock`.

**Passenger catalog / observer (2026-06-08):** galaxy-wide identity catalog of citizens seen as passengers, mirroring the seen-players pattern. Render command added same day (`passenger [<id>] [--empire X]` in play_as → `cmdPassengerCatalog` in `passenger_catalog.go`, backed by `knowledge.(*SQLiteKB).ListPassengers(ctx, citizenship)` + `GetPassenger`). NOTE: the list command is `passenger` (singular) / `passenger_catalog` — `passengers` (plural) is already an alias for `list_passengers` (aboard manifest). Pieces:
- migration **43** `add_passengers_table` → `passengers` (citizen_id PK, name, citizenship, bio, class, first/last_seen_utc, sighting_count); regenerate `scripts/sql/initialize_database.sql` via `scripts/sql/regenerate_initialize_database.sh` or `TestInitializeDatabaseSQLInSync` fails.
- `pkg/knowledge/passengers.go`: `SeenPassenger`, `RecordPassengers` (COALESCE-merge citizenship/bio/class so sparse sources don't clobber), `GetPassenger`. Added `RecordPassengers` to the `Base` interface (+ MemoryKB no-op — same gotcha as [[feedback_gameclient_interface_mocks]]).
- `pkg/game`: `ObservedPassenger` + `PassengerObserver` + `SetPassengerObserver`; `notifyPassengersFromPayload` in `passenger_notify.go` scans payload keys `waiting`/`passengers`/`loaded`/`passenger_arrivals.delivered` and is called from BOTH the TypeOK and TypeActionResult branches of `storeRawJSON`. `serverapi.PassengerRecord` is the shared identity subset.
- `pkg/agent/passenger_capture.go`: `WirePassengerObserver(c, kb)`; wired in play_as next to `WirePlayerObserver`. list_station_passengers (`waiting`) is the only source with empire+bio; others fill gaps.
