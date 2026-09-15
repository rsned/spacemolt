---
name: reference_simplehandler_drops_every_push
description: "game.SimpleHandler prints only ok/error payload messages, so every server push (the whole combat family, player_died, warnings) is decoded, mutates State, and is then dropped — invisible unless debug logging is on"
metadata:
  type: reference
---

`game.InitializeAgent` hardcodes `&SimpleHandler{}` as the message handler, and
`SimpleHandler.OnMessage` has exactly two cases: `ok` and `error`, printing
`payload["message"]`. **Every other frame is silently discarded at the
terminal.** The client *does* decode them — `handleResponse` parses
battle_damage/battle_update/battle_ended into `serverapi` structs and mutates
`State` (InCombat, InBattle, LastDamage) — and then logs them via
`c.debugLogger`, which is off unless `SetDebugLogging(true)`.

**Symptom (2026-09-14, explorer-8 at wasat_cryobelt):** a creature fight that
killed the agent showed nothing but its own command echoes and an
`out_of_ammo` error. No hit, no miss, no enemy health, no battle_ended. The
whole exchange appeared only after `set_debug true`.

## Every tool built on InitializeAgent has this hole

16 binaries call it: play_as, worker, databot, skill-runner, run-skill,
spar, duel-runner, battle-export, daily-summary, data-scraper,
faction-dashboard, auto-{trader,explorer,fighter,prophet,random}.
**Only play_as has been fixed** (`fe600938`). The worker fleet still cannot
see or react to a battle_update.

## The fix shape
`Client.SetOnPushEvent(handler) func()` (added `fe600938`, `pkg/game/subscribe.go`)
subscribes to every untagged push. Push subs **observe, never consume**, so
they don't disturb `SetOnChatMessage` / `SetOnCraftingUpdate` or any command
reply path. The handler runs synchronously inside router dispatch — keep it
fast, never issue a game command from it.

Tagged frames (RequestID set) never reach push subs at all: `dispatch` routes
those to the waiting subscription and returns before `dispatchUntagged`.

In play_as the renderer is `pushEventLines(resp, selfID) []string` in
`cmd/tools/play_as/push_events.go`; `set_events off` mutes it, because a long
fight pushes a battle_update **and** a battle_damage every single tick.

## Gotchas the renderer had to handle
- `battle_ended.winning_side` is **-1 for a stalemate** — printing the raw int
  reads as a bug.
- `battle_left` / `battle_joined` carry **no battle_id**, and a creature leaves
  with an **empty username** (the `crt_` prefixed player_id is all you get).
- `battle_update` reports hull/shield as **percentages** (`hull_pct`), unlike
  `BattleParticipant`, which carries absolutes.

[[reference_combat_damage_pipeline]] · [[reference_battle_log_api_replay_data]] ·
[[reference_rawjson_key_drift]]
