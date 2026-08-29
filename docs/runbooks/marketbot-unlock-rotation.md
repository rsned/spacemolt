# Marketbot pirate-unlock rotation (hot-swap stand-in)

`marketbot_ramens_rest` is the stand-in: it holds a resident's station while
that resident is seconded to the unlock fleet, then moves to the next one.
44 marketbots still lack the pirate unlock (the 9 stronghold bots have it).

## Invariants (read before touching anything)

- A rotating agent is listed in **both** `mb-fleet.yaml` and
  `unlock-fleet.yaml`, permanently. Fleet membership is switched **only** by
  the removed-sets in `data/overmind/<fleet>-overrides.json`. Never comment a
  yaml line out — the release becomes a no-op and the agent runs in no fleet.
- The dashboard is the writer of the overrides sidecars:
  `POST :8091/api/overmind/fleets/<fleet>/agents/<id>/remove|readd`.
- A resident takes its `station:` at spawn. Changing it in the yaml and
  sending `SIGHUP` to the mb overmind is a membership *update*: that one
  worker respawns (1 login); nothing else restarts.
- Every SIGHUP/readd costs a login; keep the IP quiet (no `temporarily
  blocked` lines in the last hour) before starting a rotation.

## Step 0 (done 2026-08-29): the stand-in's own unlock

1. `unlock-fleet.yaml` has its line, pinned to `treasure_cache_trading_post`.
2. `POST /api/overmind/fleets/mb/agents/marketbot_ramens_rest/remove`
3. `kill -HUP <unlock overmind pid>` → it spawns at the giver.
4. Graduation = `agent_standings.baseline >= 10` on all nine `pirate_*`
   factions for the agent (what the reconciler checks). Then
   `POST .../fleets/unlock/agents/marketbot_ramens_rest/remove` and
   `POST .../fleets/mb/agents/marketbot_ramens_rest/readd`.

## One rotation (agent X, station S)

1. Confirm X has a pinned line in `unlock-fleet.yaml`
   (`station: treasure_cache_trading_post`, scale-1 hull, fuel for the trip).
2. In `mb-fleet.yaml` set the stand-in's line to `station: S`;
   `kill -HUP <mb overmind pid>`; confirm in `mb-overmind.log`:
   `membership: updated marketbot_ramens_rest`, then a heartbeat at S.
3. `POST /api/overmind/fleets/mb/agents/X/remove` (docked → drains instantly).
4. `kill -HUP <unlock overmind pid>`; confirm X spawned and is routing.
5. When X graduates (step 0.4 check): remove X from unlock, readd X to mb.
   Two residents at S for a moment is harmless (double capture).
6. Next X.

Find overmind pids by scanning `/proc/*/cmdline` for
`./bin/overmind --socket data/overmind/<fleet>.sock` — never `pgrep -f`.
Automate only after two clean rotations.
