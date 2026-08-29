---
name: reference_shipping_no_active_contracts_listing
description: "Server has NO 'my active shipping contracts' listing (no shipping equivalent of get_active_missions); shipping profile returns active_contracts as a COUNT only. Dev team flagged 2026-07-23 to add a proper accepted-contracts view. Until then, disk persistence (freight-held.json) is the ONLY freight-resume source."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 86df3835-f10f-4c25-928d-7948d122ecd7
  modified: 2026-07-27T00:56:13.540Z
---

**🔴 SUPERSEDED 2026-07-26 by gameserver v0.549.0 — `shipping action=active` NOW EXISTS** and lists every contract you are party to with destination, reward, and ticks left on the deadline. Everything below describes the pre-v0.549.0 world; keep it only as the reason the resume path looks the way it does. The "WHEN IT SHIPS" plan below is now the actual TODO: add `ShippingActive` + response struct in pkg/game/serverapi and wire it into `freightReconcileSet` as the authoritative cross-check against `freight-held.json`. Disk is **no longer the only resume source**, and this is the fix for the orphan class in [[reference_freight_orphan_salvage_unpack]]. v0.549.0 also added `package_id` addressing to `get`/`track`/`deliver`/`return`, so a sealed box in the hold can be acted on with no contract ID at all. [[reference_v0549_freight_and_percrew_pirates]]

**Confirmed by server devs 2026-07-23:** the `/shipping` command has NO equivalent of `get_active_missions` — there is no "my active shipping contracts" view. `shipping --action=list` returns only shipments you are ELIGIBLE to accept (tier-filtered), and NEVER your own `in_transit` contracts. `shipping --action=profile` returns `active_contracts` as a bare integer COUNT (schema-confirmed), not a list of IDs/destinations.

**Consequence for freight resume:** a restarted carrier cannot rediscover its own in-flight contracts from the server. Local disk persistence (`<AgentsDir>/<agent-id>/freight-held.json`, [[project_freight_load_confirm_and_resume]]) is the ONLY resume source today. The reconcile mismatch detector (profile count > memory held) still fires when that file is lost/corrupted → operator rescue.

**Dev team flagged (2026-07-23) to add a proper accepted-contracts listing.** WHEN IT SHIPS: wire it into `freightReconcileSet` as a corroborating source — cross-check the disk set against the server's authoritative active list, recovering contracts the file lost. Client-side: add a `ShippingActive`/`ShippingMine` method + response struct in pkg/game/serverapi. Watch spacemolt.com/changelog / get_version for the new shipping action.

Related: [[reference_mission_board_wire_shape]] (missions DO have get_active_missions), [[project_shipping_carrier]].
