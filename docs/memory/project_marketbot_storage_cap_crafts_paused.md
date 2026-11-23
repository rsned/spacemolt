---
name: project_marketbot_storage_cap_crafts_paused
description: "Personal storage cap is 100k/item (v0.732.4 crafts obey it); 13 hourly refining crafts removed from marketbot_010/013/016/017/018 on 2026-11-22 because outputs were already over cap; restore when stock < 100k"
metadata:
  type: project
---

v0.732.4 (2026-11-22): crafting obeys per-item storage caps (job waits with pause_reason storage_full). Personal storage cap observed = **100,000 per item** (drone hosts pinned at exactly 100k: marketbot_000/001/002/007/008/ramens_rest copper_ore, _004 neon_gas, _005 nitrogen_ice, _014 lithium_ore, _sulafat iron_ore). CJ faction storage holds 500k neon, so main faction storage is not capped at 100k; the bucket is (cap_per_item 100000).

**Removed 2026-11-22 (operator approved), backups `schedule.json.bak.20261122-storagecap`:**
- marketbot_016: 11 `craft purify_argon 1200`, 12 `craft compress_hydrogen_gas 1200`, 13 `craft ionize_neon_gas 1200` (all hourly)
- marketbot_017: 11 `craft ionize_neon_gas 1200`, 12 `craft purify_argon 1200`
- marketbot_018: 11 purify_argon 1200, 12 compress_hydrogen_gas 1200, 13 ionize_neon_gas 1200
- marketbot_010: 11 `craft basic_iron_smelting 500`, 12 `craft basic_copper_processing 500`
- marketbot_013: 11 basic_iron_smelting 400, 12 basic_copper_processing 400
Stock then: purified_argon ~300k each (016/017/018), ionized_neon ~250k, LH2 ~185k (016/018), copper_wiring ~120k + steel 100-104k (010/013).

**Why:** outputs over cap make every new job pause; operator: we cannot control what drones harvest from a POI, so drone retargeting is not an option.
**How to apply:** restore a task (new id = max(ids ∪ schedule-state keys)+1) once that bot's output stock is drained below 100k (haul refined surplus to CJ faction storage or sell).

**Drones STALL at the cap (found 2026-11-22):** a drone whose hold carries a capped item cannot DEPOSIT, sits at the station idle with 100% cargo, and mines nothing at all (not just the capped share). Fix = free ~2k of the capped item in the host's storage (send_gift to another of our agents; recipient must be the in-game USERNAME from credentials.json, e.g. u_mother_trucker_100, not the agent id), then the drones unload and resume.
**Retargeted 2026-11-22** (operator-approved), scripts data/scripts/drones/mine_<poi>.ds pinned by live POI id: sulafat -> sulafat_haze (KB sulafat_outer_fields is NOT in live get_system), 001 dheneb_gas_cloud, 002 arneb_gas_pocket, 005 saiph_emission_nebula, 007 bd20_2457_ice_fields, 008 kornephoros_gas_plume, ramens_rest kornephoros_frost_ring, 014 zosma_gas_pocket. Not moved: 004 (alpheratz has one POI), 000 (gsc_0019 belts both copper/iron). Parked 2000 of the capped item in u_mother_trucker_100's storage at sulafat/dheneb/arneb/bd20_2457/kornephoros(x2)/zosma for later pickup; 005 is not gift-unlocked, so it posted a 2000 nitrogen_ice ask at 6 cr instead.
Always verify a drone target against LIVE get_system before uploading; KB POI rows can be stale.
