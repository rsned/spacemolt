---
name: reference_pirate_base_registry
description: "Known pirate/black-market station IDs, their systems, and that they only live (stale) in knowledge.db, not market.db"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 868ac572-f106-4923-9823-e82622bf66b9
  modified: 2026-08-13T00:09:51.967Z
---

Pirate / black-market stations require pirate access (smuggling skill) to reach — only **craftsman-1** currently has it. They are the market-data blind spot: marketbots never reach them, so they appear ONLY in `data/spacemolt-knowledge.db` (`market_buy_orders`/`market_sell_orders`, DELETE+INSERT → keeps the *last* snapshot per station forever) and are **absent from `data/market.db`** (its 4h rolling prune drops any station not re-captured recently).

MARKET EPOCH: the server did a **server-wide market reboot ~2026-06-24** (aligns with the `data/backups/market.20260623.db` snapshot). Any knowledge.db capture BEFORE ~06-24 is from a DEAD epoch — prices are invalid, not merely stale. Clean gap in capture history: 06-06, 06-12, then nothing until 06-29. PURGE TOOL: `cmd/data/purge-preepoch` (dry-run by default, `--apply`, `--cutoff <RFC3339>`) deletes pre-cutoff rows from knowledge market_buy_orders/market_sell_orders — reusable at each reboot. RAN 2026-07-01 @ cutoff 2026-06-24T00:00:00Z: purged 370 dead-epoch rows (voss_redoubt 104, kael_arsenal 193+73). `demand` staleness tag now renders `station (Nt ago[, STALE])` (commit 4689156).

**⭐ ALL NINE STRONGHOLDS ARE IDENTIFIED (2026-08-12) — none is "undiscovered".**
`agent_standings` carries exactly nine `pirate_*` factions, and every stronghold
POI is already in `data/spacemolt-knowledge.db`. **The stronghold marketbot is
named for its target SYSTEM**, which gives a clean 1:1 posting map:

| system | stronghold POI | warlord | marketbot |
|---|---|---|---|
| alhena | `voss_redoubt` | Voss | marketbot_alhena |
| barnard_44 | `sable_port` | Sable | marketbot_barnard_44 |
| bellatrix | `crix_stronghold` | Crix | marketbot_bellatrix |
| xamidimura | `kael_arsenal` | Kael | marketbot_xamidimura |
| algol | `dross_citadel` | Dross | marketbot_algol |
| gliese_581 | `korr_fortress` | Korr | marketbot_gliese_581 |
| gsc_0008 | `nyx_nexus` | Nyx | marketbot_gsc_0008 |
| sheratan | `thane_keep` | Thane | marketbot_sheratan |
| zaniah | `mera_sanctum` | Mera | marketbot_zaniah |

Korr/Dross/Mera were recorded below as UNDISCOVERED. That was about **market
capture**, not identity: `korr_fortress`, `dross_citadel`, `mera_sanctum`,
`nyx_nexus` and `thane_keep` have never been scanned, so they hold no price
data, but we know exactly where each one is. Query with
`select id from pois where system_id=? and type='station'`.

Deployment is therefore fully specified: each marketbot flies to its own system
and goes resident (`role: resident, station: ""` in `mb-fleet.yaml`) at its
stronghold. Gated only on pirate baseline ≥ 10
([[project_pirate_reputation_unlock_campaign]]).

WARLORD ROSTER (from in-game radio broadcast, 2026-07-01) — each warlord has a self-styled rank and owns one base:
- **Sable** (Director) → `sable_port` / Barnard 44 — KNOWN
- **Voss** (Commandant) → `voss_redoubt` / Alhena — KNOWN
- **Kael** (Admiral) → `kael_arsenal` / Xamidimura — KNOWN
- **Crix** (Sovereign) → `crix_stronghold` / bellatrix — KNOWN
- **Korr** (Grand Marshal) → `korr_*` — UNDISCOVERED (no capture yet)
- **Dross** (Imperator) → `dross_*` — UNDISCOVERED
- **Mera** (Archon) → `mera_*` — UNDISCOVERED
≥7 warlords total; 3 bases (Korr/Dross/Mera) never scanned — dark high-value pirate markets. Scout with craftsman-1 (only smuggling-access agent).

NAMING TELL: pirate bases are named for the **warlord who owns them** — `<warlord>_<structure>` (Sable→sable_port, Crix→crix_stronghold, Kael→kael_arsenal, Voss→voss_redoubt; structures = stronghold/arsenal/redoubt/port). This is how to spot a pirate base vs. an empire station, which follows `<system>_<civic-suffix>` (colonial_station, processing_station, waystation, etc.). A warlord-named station_id ⇒ pirate/black-market, reachable only with smuggling access.

Confirmed pirate bases (user-verified 2026-07-01), `station_id` → `system_id` (warlord):
- `sable_port` → **Barnard 44** — POST-reboot (07-01), ✅ current. Server payload calls it `sable_port_station` but BOTH stores normalize/persist as `sable_port` (query the short id). High-value: Enriched Uranium Rod @ ~6,813.
- `crix_stronghold` → **bellatrix** — POST-reboot (06-29), aging but valid. High-value: Galactic Standard Alloy @ ~5,111. Same stronghold that stranded a hauler (Crix routing loss). [[project_haul_lawless_routing]]
- `kael_arsenal` → **Xamidimura** — PRE-reboot (06-12), ❌ DEAD epoch, discard. Low-value ammo/power: Power Cell @ 549, Ghost Rounds.
- `voss_redoubt` → **Alhena** — PRE-reboot (06-06), ❌ DEAD epoch, discard. Low-value ammo: Ghost Rounds @ 639.

Tier split: sable_port + crix buy refined goods (uranium rods, alloys @ 5–7k → the trader-9 pirate-sale target, ~420k); kael_arsenal + voss_redoubt are ammo/power-cell markets (400–650).

NOT a pirate base — `mobile_capital` is the **Outerrim Empire capital**, a station that JUMPS systems once per day throughout the empire's systems. Its captured `system_id` (Void Gate on the 06-30 snapshot) is just where it was that day and will keep changing — do not treat its system as fixed. [[project_agent_empire_bands]] (outerrim = trailing 9-10). Big market (~955 orders).

Still-unclassified knowledge-only stations: `ironhearth_station` (ironhearth), `market_prime_exchange` (market_prime — likely the normal marketbot station, not pirate).

FACTION FACILITIES (2026-07-02): the stronghold-access "pirate friends" faction (craftsman-1's, the only smuggling-access agent) OWNS a full faction facility spread incl. a **faction_storage Faction Warehouse** (level 2, capacity 200k, ~329k credits, at grand_exchange_station) plus intel/market/fuel/refineries/admin. It is NOT in an overmind pool. This means the faction TREASURY path (deposit/withdraw) is live for THIS faction — relevant to the hauler working-capital recap spec (`docs/superpowers/specs/2026-07-02-hauler-working-capital-floor-design.md`): treasury withdraw is viable where a faction has storage AND the worker is docked at the warehouse, but is location-bound; the overmind haul pools' factions have NO storage, so peer send_gift stays their primary recap path. faction_list now returns this context (base_id/faction_id/faction_facilities/faction_storage/hint) — serverapi FactionListResponse updated 2026-07-02.

To refresh any of these, craftsman-1 must dock at the specific station and run `update_market`. A bellatrix visit does NOT auto-capture crix_stronghold unless docked there. The `demand` tool renders staleness now as `station (Nt ago[, STALE])` — see [[project_current_status]]; pirate bases show as `(...t ago, STALE)` unless just refreshed.
