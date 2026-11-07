---
name: reference_poi_rename_stale_rows
description: Resource POIs were renamed around 2026-10-13 for more themed names, leaving old rows in the KB that double-count belts and nebulae
metadata:
  type: reference
---

The server renamed POIs around 2026-10-13 (ticks near 2128700) to get more
themed names, such as `silicon_cluster_barnard_44` to `clean_scar_cluster` and
`crystal_fragment_field_gliese_581` to `fortress_glitter`. The KB keeps the old
rows, so counting POIs per system double-counts those sites.

**How to apply:** when counting a system's sites, ignore rows whose
`last_updated_tick` is far behind that system's other rows. Trust a live
`get_system` over the KB. Old rows still hold real `poi_resources`, which show
what the renamed site holds. Not every old row is a rename: some systems may
simply not have been visited since.

[[reference_public_stations_api]]
