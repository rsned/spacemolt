---
name: reference_catalog_ship_stats_include_default_modules
description: "catalog_ships base_speed can already include a default module's bonus (Frankenhauler speed 2 = speed-1 hull + default afterburner_i); strip default_modules before comparing hulls"
metadata:
  node_type: memory
  type: reference
  originSessionId: 8c15f693-8825-4572-8f22-51141db1e328
  modified: 2026-11-19T20:57:38.998Z
---

The ship catalog's `base_speed` is not always the bare hull. The Frankenhauler reads
`base_speed: 2`, but that comes from its default `afterburner_i` (+1 speed, worse fuel
economy); the hull itself is speed 1 (operator, 2026-11-19).

**How to apply:** when comparing hulls for range or throughput, read `default_modules`
and back out what they add before using `base_speed` (and possibly `cargo_capacity` /
`base_fuel`, if a default expander or fuel tank is fitted; unverified for those). Removing
a default module changes both speed and fuel per jump, and frees its utility slot.
The fuel and jump-time formulas are in [[reference_ship_jump_time_and_fuel_formulas]].
