---
name: project_passenger_demand_intel
description: Feature — resident marketbots survey list_station_passengers galaxy-wide into a passenger_demand KB table so the shuttle can route to real demand
metadata: 
  node_type: memory
  type: project
  originSessionId: 868ac572-f106-4923-9823-e82622bf66b9
---

**Feature (requested 2026-06-28, scope = recurring resident intel):** the shuttle canary johnny_cab found ZERO passengers across 131 passes incl. at Haven grand_exchange (see [[project_treasury_and_shuttle]]). We don't know WHERE passengers are. Fix: use the ~34 resident marketbots (one per station) as distributed scouts to call `list_station_passengers` on their scheduled scan, persisting a galaxy-wide passenger-demand map. Same distributed-intel pattern as market.db + ship_listings.

**Integration points (verified 2026-06-28):**
- Client method EXISTS: `Client.ListStationPassengers(ctx, station string) (*serverapi.ListStationPassengersResponse, error)` (`pkg/game/client_passengers.go:30`); response has `Passengers []serverapi.StationPassenger` = `{CitizenID, Name, Citizenship, Class, Destination, DestinationName, DestinationSystem, EstimatedFare int}`. Pass `""` for the currently-docked station.
- Persistence hook: add capture beside `KBUpdateStation` (`pkg/worker/capture.go:495`, the browse_ships→`StoreShipListings` per-station hook called by `KBUpdateAll` at :694). New `KBUpdatePassengers(ctx, client, kb, source)`.
- New KB table `station_passenger_demand` via `pkg/knowledge/sqlite_migrations.go` (uses `CREATE TABLE IF NOT EXISTS` collapse style). Suggested cols: origin_station_id, origin_system_id, dest_system, dest_station, class, passenger_count, total_estimated_fare, captured_tick, captured_at. Add `knowledge.Base` methods `StorePassengerDemand` + `GetPassengerDemand`/`GetAllLatestPassengerDemand` (mirror the ship_listings methods — note [[reference_ship_replacement_workflow]] uses GetAllLatestShipListings as a model; same interface-break-breaks-mocks caveat [[feedback_gameclient_interface_mocks]]).
- Dispatch task: `pkg/worker/dispatch.go` (:52 query-allowlist, :157 `kb_update` case) — either fold the passenger scan into `kb_update`/`KBUpdateAll` (simplest — residents already run it hourly) or add a dedicated `passenger_scan` task + schedule entry.
- Deploy: rolling-restart the 34 marketbots onto the new binary ([[reference_overmind_launch_commands]], stagger; fresh logins not gated per [[reference_login_vs_reconnect_gating]]).

**Consumer (later):** shuttle routing reads `GetAllLatestPassengerDemand` to pick a destination system with real demand instead of hunting blind; also fixes the reposition picker that currently targets station-less systems (Bunda/Alrakis/Alula/Copernicus confirmed station-less).

**Status:** NOT STARTED — scoped 2026-06-28 while the haul ship-replacement SDD build (plan `docs/superpowers/plans/2026-06-28-haul-ship-replacement.md`) was in flight.
