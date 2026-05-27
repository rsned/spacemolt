# Factions

# Overview


# Creating

# Building out

# hints

factions need a faction_lockbox before any other facilities can be built




$ facility faction_build --facility_type faction_quarters
▶ Executing: facility faction_build --facility_type faction_quarters
[craftsman-1-GAME] 2026/05/22 20:31:27 === Game Client Send Debug ===
[craftsman-1-GAME] 2026/05/22 20:31:27 Message Type: 'facility'
[craftsman-1-GAME] 2026/05/22 20:31:27 Message Payload: {"action":"faction_build","facility_type":"faction_quarters"}
[craftsman-1-GAME] 2026/05/22 20:31:27 === Game Client Receive Debug ===
[craftsman-1-GAME] 2026/05/22 20:31:27 Response Type: 'ok'
[craftsman-1-GAME] 2026/05/22 20:31:27 Response Payload: {"command":"facility","message":"Action 'facility' pending. Will execute on next tick.","pending":true}
[craftsman-1-GAME] 2026/05/22 20:31:27 Response Message: 'Action 'facility' pending. Will execute on next tick.'
[PLAY_AS-craftsman-1] 2026/05/22 20:31:27 OK: Action 'facility' pending. Will execute on next tick.
[craftsman-1-GAME] 2026/05/22 20:31:35 === Game Client Receive Debug ===
[craftsman-1-GAME] 2026/05/22 20:31:35 Response Type: 'action_result'
[craftsman-1-GAME] 2026/05/22 20:31:35 Response Payload: {"command":"facility","result":{"action":"faction_build","base_id":"grand_exchange_station","capacity":1,"facility_id":"7913a3713938fb11221f19b93a32ec86","facility_name":"Faction Quarters","facility_type":"faction_quarters","faction_service":"faction_commons","hint":"Facility is under construction. Use action 'faction_list' to check progress.","members_awarded_xp":1,"rent_per_cycle":0,"skill_xp":{"corporation_management":750},"under_construction":true},"tick":895121}
[craftsman-1-GAME] 2026/05/22 20:31:35 Stored raw JSON for facility (471 bytes)
[craftsman-1-GAME] 2026/05/22 20:31:35 Action result: facility (unhandled)

🏗  Built faction facility: Faction Quarters (faction_quarters)
  Base:        grand_exchange_station
  Facility ID: 7913a3713938fb11221f19b93a32ec86
  Service:     faction_commons
  Rent/Cycle:  0
  Status:      Under construction

 +750 xp corporation_management
 (awarded to 1 member(s))

ℹ Facility is under construction. Use action 'faction_list' to check progress.




$ facility faction_list
▶ Executing: facility faction_list
[craftsman-1-GAME] 2026/05/22 20:33:22 === Game Client Send Debug ===
[craftsman-1-GAME] 2026/05/22 20:33:22 Message Type: 'facility'
[craftsman-1-GAME] 2026/05/22 20:33:22 Message Payload: {"action":"faction_list"}
[craftsman-1-GAME] 2026/05/22 20:33:23 === Game Client Receive Debug ===
[craftsman-1-GAME] 2026/05/22 20:33:23 Response Type: 'ok'
[craftsman-1-GAME] 2026/05/22 20:33:23 Response Payload: {"base_id":"grand_exchange_station","faction_facilities":[{"active":true,"facility_id":"077a6773b401e4bd4737ab9649726f12","faction_service":"faction_admin","level":1,"name":"Faction Desk","rent_per_cycle":0,"status":"active","type":"faction_desk"},{"active":true,"capacity":100000,"facility_id":"c798f71e1a3811bab005422d8d352ddb","faction_service":"faction_storage","level":1,"name":"Faction Lockbox","rent_per_cycle":0,"status":"active","type":"faction_lockbox"},{"active":true,"facility_id":"30da5dea4cf608bcee3b9668da6b22bb","faction_service":"faction_trade_intel","level":1,"name":"Trade Ledger","rent_per_cycle":0,"status":"active","type":"trade_ledger"},{"active":false,"capacity":1,"facility_id":"7913a3713938fb11221f19b93a32ec86","faction_service":"faction_commons","level":1,"name":"Faction Quarters","rent_per_cycle":0,"status":"under_construction","ticks_until_complete":20,"type":"faction_quarters"}],"faction_id":"e727c0e918d994c72db2978fe5b18edc","faction_storage":{"credits":150000,"item_types":6,"rooms":0},"hint":"Use action 'faction_build' to build new faction facilities. Faction Storage is required before any other faction facility."}
[craftsman-1-GAME] 2026/05/22 20:33:23 Stored raw JSON for facility (1155 bytes)

  Faction Facilities at grand_exchange_station
    Faction:  e727c0e918d994c72db2978fe5b18edc
    Storage:  150,000 cr | 6 item type(s) | 0 room(s)

  Name             | Type             | Service             | Lvl | Status             | Capacity | Rent/cycle
  -----------------+------------------+---------------------+-----+--------------------+----------+-----------
  Faction Desk     | faction_desk     | faction_admin       |   1 | active             |        0 |          0
  Faction Lockbox  | faction_lockbox  | faction_storage     |   1 | active             |  100,000 |          0
  Faction Quarters | faction_quarters | faction_commons     |   1 | under_construction |        1 |          0
  Trade Ledger     | trade_ledger     | faction_trade_intel |   1 | active             |        0 |          0


