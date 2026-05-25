# spar

Controlled PvP sparring harness. Logs in two or more of our own agents, equips
and moves them to a non-empire arena (PvP is anything-goes outside empire
space), and runs scripted or human-partnered battles so the combat mechanics
are observable and testable.

## Usage

```
spar [flags] <agent-1> <agent-2> [agent-3 ...]
```

`ross_128` is lawless, one jump from the station system `treasure_cache`.

```bash
# Two bots fight; fighter-1 (aggressor) attacks fighter-2 (skirmisher).
spar --arena ross_128 fighter-1 fighter-2

# Practice yourself against a passive dummy. The harness stages & drives
# fighter-2 only; you separately run `play_as <your-agent>`, travel to the
# arena rendezvous POI, and `attack <fighter-2-username>`.
spar --mode partner --arena ross_128 --policy fighter-2=dummy fighter-2
```

`--arena` is required for now (auto-discovery is deferred).

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `botvbot` | `botvbot` (all scripted) or `partner` (you join via play_as). |
| `--arena` | — | Arena system id, **required** (e.g. `ross_128`); auto-discovery deferred. |
| `--policy` | — | Per-agent policies, e.g. `fighter-1=aggressor,fighter-2=dummy`. |
| `--aggressor` | first | Which bot initiates (botvbot). |
| `--rendezvous` | `asteroid_belt` | POI type to gather combatants at. |
| `--max-ticks` | `60` | Safety cap on match length. |
| `--no-equip` | `false` | Skip auto-equip (verify only). |
| `--weapon` / `--shield` | `pulse_laser_i` / `shield_booster_i` | Cheap gear fitted if missing. |
| `--debug` | `false` | Debug logging. |

## Stopping a match

Press Ctrl-C to stop early: the harness tells each bot to `flee`, then prints the
partial telemetry summary before exiting. A match also ends on its own when one
side is wiped out or `--max-ticks` is reached.

Auto-rebuild of a destroyed ship between matches (`--rebuild`) is not yet
implemented — re-run the binary to start a fresh match.

## Policies

- **aggressor** — advance to `engaged`, target nearest, hold `fire`.
- **skirmisher** — hold `mid` and fire; retreat a zone when hull < 40%.
- **retreater** — adopt `flee` immediately (exercises the multi-tick escape).
- **dummy** — `brace` only; never advance or fire (low-risk practice partner).

Stakes: fights can run to completion (ships can die) — use cheap/throwaway
loadouts. Arena must be outside empire space.
