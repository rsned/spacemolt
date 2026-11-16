# Beid station (station #2): record set

The raw record of founding and running our second player station, in Beid,
with an outpost next door in Ironpeak. Design:
`docs/superpowers/specs/2026-11-15-station-two-and-radioactive-outpost-design.md`.
Plan: `docs/superpowers/plans/2026-11-16-station-two.md`.

| File | Written by | What it holds |
|---|---|---|
| `events.jsonl` | play_as `--record-dir` | Every command run, its args, game tick, error (if any), and the server's raw reply. Reads from one `station_snapshot` share a `snapshot_id`. |
| `ledger.jsonl` | play_as `ledger` | Every credit or item movement, categorised (transfer for funding, founding, build, maintenance-items, maintenance-labor, ammo, haul-in, purchase, revenue, transfer). Negative = out. |
| `findings.md` | by hand | One entry per fact learned, tagged **measured** or **doc-quoted**, with date. Starts with the pre-flight notes. |

## Running

```
go run ./cmd/tools/play_as --record-dir data/stations/beid craftsman-2
```

craftsman-2 (Samuel 'Smith' Sterling, CRFT officer) is the lead.

Inside play_as:

- `station_snapshot`: run at the **start and end of every session**, and at least
  **hourly** while one is running.
- `ledger purchase power_cell 200 bought at Haven`
- `ledger founding credits -5000000`

## Rules

- Never `git add -A` (the tree has dirty agent data). Commit this directory by path.
- Field names come from the recorded raw replies, not from the docs.
- Faction storage is PER STATION: CJ's stock is not usable here until hauled.
- Treasury floor 1M; consumable purchases ≤1M total; each facility build is approved first.
