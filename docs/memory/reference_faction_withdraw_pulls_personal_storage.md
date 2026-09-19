---
name: reference_faction_withdraw_pulls_personal_storage
description: "faction_withdraw_items target is an enum: self = YOUR storage, faction = the lockbox. --target=self succeeds silently against personal stock and leaves faction untouched; use target=faction"
metadata:
  node_type: memory
  type: reference
---

⭐🔴 **`faction_withdraw_items <item> <n> --target=self` pulls from the agent's
OWN station storage, NOT the faction lockbox.** It succeeds. It returns a
sensible-looking result. The faction balance does not move.

Observed 2026-09-19, craftsman-boss docked at `grand_exchange_station`:

| source | trade_crystal before | after |
|---|---:|---:|
| personal (`view_storage`) | 881 | **781** |
| faction lockbox | 63,500 | **63,500 (unchanged)** |

```
faction_withdraw_items trade_crystal 100 --target=self
-> {"action":"withdraw_items", "quantity":100, "storage_remaining":781, "cargo_total":100}
```

**Three tells, all visible in the reply:**
1. `storage_remaining` matches PERSONAL stock minus the withdrawal (881-100),
   not faction stock minus it (would be 63,400).
2. The echoed `"action"` is plain **`withdraw_items`** — the server did not
   echo back the faction variant it was asked for.
3. Faction stock re-read afterwards is byte-identical.

## RESOLVED the same day: the answer is `target="faction"`

`target` is an ENUM with exactly two legal values, and the server names them
when you get it wrong:

```
faction_withdraw_items trade_cipher 15 --target=storage
-> action_error invalid_target: "Cannot withdraw from another player's
   storage. Use target=\"self\" or target=\"faction\"."
```

| target | source |
|---|---|
| `self` | **your own** station storage (the default-looking trap) |
| `faction` | **the faction lockbox** — what the command name implies |

So nothing was broken: `self` did exactly what it says. The trap is purely that
the command is NAMED `faction_withdraw_items`, so `--target=self` reads as
"from the faction, to me" when it actually means "from my own storage". Faction
stock IS reachable — use `target=faction`.

Wire shape: `{"item_id":...,"quantity":N,"target":"faction"}`. The reply
normalises `action` to plain `withdraw_items` either way, so **the action name
cannot tell you which source was used** — only `storage_remaining` against a
known balance can, and that is what made the mistake invisible.

Same failure class as [[reference_chat_target_id_conversation_key]] and
[[reference_station_id_aliases]]: the command is accepted, the reply looks
right, and the wrong object was touched. Always verify the ledger you meant to
change actually changed.

**Faction lockbox @ grand_exchange_station (capture 2026-09-19T00:11Z)** — much
larger than expected and relevant to live projects: trade_crystal 63,500 ·
**liquid_hydrogen 38,097** (=~19,000 fuel_cells at 2 LH each, see
[[project_no_fuel_cells_refuel_deadlock]]) · argon_gas 20,295 · neon_gas 20,000
· hydrogen_gas 8,648 · silicon_ore 8,010 · steel_plate 3,150 · circuit_board
1,900 · flex_polymer 1,940 (the last two cover ~630 `build_light_drone_bay`).
