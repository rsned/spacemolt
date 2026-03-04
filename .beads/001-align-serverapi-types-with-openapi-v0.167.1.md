# Bead 001: Align serverapi types with OpenAPI v0.167.1 and purge stale entity IDs

**Status:** Completed
**Date:** 2026-03-04
**Triggered by:** SpaceMolt Gameserver v0.167.1 release notes announcing the OpenAPI spec now accurately documents all response field shapes (previously many showed as `{}`)

---

## Background & Motivation

The spacemolt codebase communicates with the game server over WebSocket, deserializing JSON responses into Go structs defined in `pkg/game/serverapi/`. When these structs were originally written, the OpenAPI spec at `server_docs/openapi.json` had incomplete type information -- many nested objects showed as `{}` with no properties. As a pragmatic workaround, **38 fields across the codebase used `map[string]any`** as a catch-all for untyped server data.

With v0.167.1, the server team documented every response field properly. This created two problems:

1. **Type safety gap:** Our Go types were weaker than what the server actually guarantees. Code accessing these fields required unsafe type assertions (`ship["id"].(string)`) instead of direct struct field access (`ship.ShipID`). This is fragile, produces runtime panics on type mismatches, and makes refactoring dangerous.

2. **Schema drift:** Several response types had outright wrong field names, missing fields, or incorrect types that had accumulated over time as the server evolved but the Go types weren't updated.

Additionally, the game server had previously migrated all entity IDs from old formats (`sys_0278`, `sol_base`, `haven_base`) to new human-readable names (`zavijava`, `grand_exchange_station`). Our SQLite knowledge base still contained 506 systems, 1144 POIs, and 5 bases with the old IDs, making them useless for lookups against current server data.

## What Changed

### Part 1: serverapi type alignment (~1,600 lines changed)

**New sub-types added to `types.go` (30+ structs):**
These replace `map[string]any` fields with proper Go structs. Each mirrors the exact shape documented in the OpenAPI spec:

- **Combat:** `ActiveBuff`, `BattleParticipant`, `BattleSide`, `CombatLogEntry`
- **Market/Trading:** `MarketInsight`, `OrderFill`, `AutoListedOrder`, `TradeDetail`, `TradeItem`
- **Insurance:** `InsuranceQuote`, `InsuranceFactor`, `InsurancePolicy`
- **Commissions:** `CommissionDetail`
- **Ship listings:** `OwnedShip`, `ShipListingDetail`, `ShowroomShip`
- **Surveys:** `RevealedPOI`, `SurveyResource`, `FaintSignature`
- **Storage:** `StorageShip`, `StorageGift`, `StorageGiftItem`, `StorageGiftShip`
- **Info/UI:** `GameCommand`, `ChangelogVersion`, `GuideEntry`, `StationType`, `ActionLogEntry`
- **Forums:** `ForumThreadSummary`, `ForumReplyDetail`
- **Missions:** `MissionBoardEntry`, `ActiveMission`, `ActiveMissionObjective`, `ActiveMissionProgress`, `MissionGiver`, `MissionRequirements`
- **Factions:** `FactionSummary`, `FactionMemberDetail`, `FactionWarDetail`
- **Modules:** `ShipModule` (detailed installed module with all stat fields)
- **Login:** `PendingTradeInfo`, `ReleaseInfo`, `UnreadChat`
- **Station:** `StationHealth`, `NearbyPirate`, `ActiveRaid`

**Existing types updated:**
- `CargoItem`: Added `Name`, `Size` fields (spec shows cargo items include display info)
- `Ship.ActiveBuffs`: `[]map[string]any` -> `[]ActiveBuff`
- `Storage.Ships/Gifts`: `[]map[string]any` -> `[]StorageShip`/`[]StorageGift`
- `ExchangeOrder`: Renamed `ID`->`OrderID`, `Type`->`OrderType`; added `Side`, `Remaining`, `FilledQuantity`, `ItemName`, `ListingFee`, `CreatedBy`, `FactionOrder`
- `ViewMarketItem`: Added `Category`, `BuyPrice`, `BuyQuantity`, `SellPrice`, `SellQuantity`, `Spread`
- `MarketOrder`: Added `MyQuantity`, `Source`
- `Wreck`: Completely restructured -- added `Type`, `SystemID`, `VictimID/Name`, `KillerID/Name`, `Cargo` (was `Contents`), `Modules`, `SalvageValue`, `InsurancePolicyID`, `TowedByPlayerID`, `ExpireTick`
- `Drone`: `ID` -> `DroneID` (matching server's `drone_id` field)
- `Note`: `ID` -> `NoteID`, added `UpdatedAt`
- `SystemSearchResult`: `ID` -> `SystemID` (matching server's `system_id` field)
- `PlayerDied.CombatLog`: `[]map[string]any` -> `[]CombatLogEntry`

**Response types updated/added in `responses.go` (~35 updated, ~40 new):**

Key structural changes (not just field additions):
- `AnalyzeMarketResponse`: Completely restructured -- `TopInsights []map[string]any` -> `Insights []MarketInsight`; removed `Mode`, `ScanningRange`, `Analysis`; added `Station`
- `EstimatePurchaseResponse`: `ItemID` -> `Item`, `Quantity` -> `QuantityRequested`, `TotalCost float64` -> `TotalCost int`; added `Fills`, `Unfilled`
- `GetTradesResponse`: `Trades []map[string]any` -> `Incoming`/`Outgoing []TradeDetail` (server returns two separate arrays, not one)
- `GetMissionsResponse`: `Missions []map[string]any` -> `[]MissionBoardEntry`
- `GetActiveMissionsResponse`: `Missions []map[string]any` -> `[]ActiveMission`; added `TotalCount`, `MaxMissions`
- `FactionInfoResponse`: `Faction map[string]any` -> flat struct with 20+ typed fields
- `LoginResponse`: `CaptainsLog`/`PendingTrades` now typed; added `Message`, `ReleaseInfo`, `UnreadChat`
- `CommissionQuoteResponse`: `Quote map[string]any` -> 12+ flat typed fields
- `ListShipsResponse`: `Ships []map[string]any` -> `[]OwnedShip`
- `ViewMarketResponse.Base`: `map[string]any` -> `string`
- `ViewOrdersResponse`: Added `FactionOrders`, `OrdersCount`, `Hint`, etc.
- `FindRouteResponse`: Added `Found`, `TotalJumps`, `TargetSystem`, `Message`; `Route` now optional
- `SurveySystemResponse`: Completely restructured with typed survey fields
- `GetVersionResponse`: `ReleaseNotes` -> `Notes`, `Changelog` -> `Versions []ChangelogVersion`
- `GetCargoResponse`: `Used`/`Capacity`/`Available` changed from `float64` to `int`
- `GetStatusResponse`: Added `Modules []ShipModule`
- `GetBaseResponse`: Added `Condition *StationHealth`, `Services` now required
- `SearchChangelogResponse`: `Results []map[string]any` -> `Releases []ChangelogVersion`

New response types added: `GetShipResponse`, `GetNearbyResponse`, `BuyResponse`, `SellResponse`, `CreateBuyOrderResponse`, `CreateSellOrderResponse`, `CancelOrderResponse`, `ModifyOrderResponse`, `TradeOfferResponse`, `TradeAcceptResponse`, `AcceptMissionResponse`, `CompleteMissionResponse`, `BuyInsuranceResponse`, `CommissionShipResponse`, `BuyShipResponse`, `SellShipResponse`, `SwitchShipResponse`, `BuyListedShipResponse`, `ListShipForSaleResponse`, `CreateNoteResponse`, `WriteNoteResponse`, `CraftResponse`, `CloakResponse`, `ScanResponse`, `JettisonResponse`, `InstallModResponse`, `UninstallModResponse`, `ReloadResponse`, `UseItemResponse`, `RefuelResponse`, `RepairResponse`, `MineResponse`, `LootWreckResponse`, `SalvageWreckResponse`, `TowWreckResponse`, `SellWreckResponse`, `DeployDroneResponse`, `BuildBaseResponse`, `AttackBaseResponse`, `TravelResponse`, `DockResponse`, `UndockResponse`, `ChatResponse`, `RegisterResponse`, `HelpResponse`, `FacilityListResponse`, `RaidStatusResponse`, `GetActionLogResponse`, `GetBaseCostResponse`, `SetAnonymousResponse`, `SetColorsResponse`, `SetStatusResponse`, `SetHomeBaseResponse`, `PendingActionResponse`, `MessageResponse`, and 15+ faction-specific responses.

**Callers fixed:**
- `convert.go`: `ActiveBuff` conversion rewritten from map iteration to struct field access
- `auto-trader/main.go`: `ListShipsResponse` usage updated from `ship["id"].(string)` to `ship.ShipID` pattern; "stored" status check changed to `!ship.IsActive`
- `events.go`: `PlayerDied.CombatLog` type updated

### Part 2: Database purge and re-import

**Backup:** `data/backups/spacemolt-knowledge-20260304-093546.db` (3.4MB snapshot via SQLite `.backup`)

**Tables dropped and recreated:**
| Table | Old rows | New rows | Notes |
|-------|----------|----------|-------|
| systems | 506 | 505 | Old `sys_0278` -> new `zavijava` style |
| connections | 2140 | 2130 | Re-derived from new map data |
| pois | 1144 | 1 | Will repopulate as agents explore |
| poi_resources | 1109 | 0 | Will repopulate as agents mine |
| bases | 5 | 2 | Old `sol_base` -> new `grand_exchange_station` |
| base_services | 45 | 0 | get_base response doesn't export services |
| base_facilities | 83 | 54 | From two imported bases |
| base_market | 24 | 0 | Market data is dynamic |

**Data sources:**
- Map: `data/game-api/craftsman-1/get_map.json` (505 systems)
- Bases: `craftsman-1/get_base.json` (Grand Exchange Station) + `salvager-9/get_base.json` (Frontier Station)

## Design Decisions & Rationale

### Why not use the OpenAPI spec to auto-generate Go types?
The serverapi types serve as a translation layer between the raw server JSON and the internal game types (`pkg/game/types.go`). Auto-generation would:
- Produce types that don't match Go naming conventions
- Require a build dependency on an OpenAPI code generator
- Generate types for 145+ responses when only ~46 are actively used
- Make it harder to add custom `UnmarshalJSON` methods (like `Skill` has)

Hand-maintained types let us be selective about what we type strongly vs. leave as `map[string]any` (e.g., `FacilityResponse.Actions` stays untyped because facility actions are highly polymorphic and rarely deserialized into specific types).

### Why keep some `map[string]any` fields?
A few fields intentionally remain untyped:
- `FacilityResponse.Actions/Types/Upgrades` - Polymorphic facility data that varies by facility type
- `ViewFactionStorageResponse.RecentActivity` - Activity log entries with variable structure
- `SellShipResponse.ModulesToStorage` - Module summary format unclear in spec
- `ActionLogEntry.Data` - Arbitrary event-specific data

### Why drop+recreate tables instead of UPDATE?
The old IDs (`sys_0278`) and new IDs (`zavijava`) have no mapping table. The server just renamed everything. A row-by-row update would require knowing `sys_0278 = zavijava`, which we don't have. Clean slate is the only option. POIs and resources will repopulate naturally as agents explore.

### Why only 2 bases imported?
The import tool takes individual `get_base.json` files. We only had current-ID base data for Grand Exchange Station and Frontier Station. The other 3 old bases (Confederacy Central Command, Central Nexus, Crimson War Citadel) need fresh `get_base` calls from agents docked at those stations to get their new IDs.

## Verification

- `go build ./...` -- clean
- `go test ./...` -- all pass (including `pkg/game` at 87s)
- `golangci-lint run ./pkg/game/... ./cmd/auto-trader/...` -- 0 issues
- Database queries confirm 0 old-style IDs remain
- New system IDs verified against live map data format

## Files Changed

```
cmd/auto-trader/main.go             |   11 +-   (ListShipsResponse caller update)
pkg/game/convert.go                 |   19 +-   (ActiveBuff conversion)
pkg/game/serverapi/events.go        |    2 +-   (CombatLog type)
pkg/game/serverapi/responses.go     | 1121 +++  (35 updated + 40 new response types)
pkg/game/serverapi/types.go         |  607 +++  (30+ new sub-types, existing type updates)
data/backups/spacemolt-knowledge-*.db           (database backup)
data/spacemolt-knowledge.db                     (rebuilt tables)
```

## Follow-up Work

1. **Dock agents at remaining bases** to capture fresh `get_base.json` with new IDs for: Confederacy Central Command (Sol), Central Nexus, Crimson War Citadel (Krynn)
2. **Consider adding a `data-scraper` run** to refresh all game-api cached JSON files with current server data
3. **Update internal `pkg/game/types.go`** to also use typed buffs/storage (currently still `[]map[string]any` on the internal side; only the serverapi layer was updated)
4. **Add deserialization tests** for the new response types against sample server JSON to catch drift early
5. **Port `ExchangeOrder` field rename** through any knowledge base code that references the old `id`/`type` field names
