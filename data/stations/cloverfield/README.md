# Cloverfield station: record set

The raw record of founding and running our first player station in
Cloverfield. The KB station guide and the station HMI are both built from
these files. Design: `docs/superpowers/specs/2026-10-25-cloverfield-home-station-design.md`.
Plan: `docs/superpowers/plans/2026-10-25-cloverfield-home-station.md`.

| File | Written by | What it holds |
|---|---|---|
| `events.jsonl` | play_as `--record-dir` | Every command run, its args, game tick, error (if any), and the server's raw reply. Reads from one `station_snapshot` share a `snapshot_id`. |
| `ledger.jsonl` | play_as `ledger` | Every credit or item movement, categorised (founding, build, maintenance-items, maintenance-labor, ammo, haul-in, purchase, revenue, transfer). Negative = out. |
| `findings.md` | by hand | One entry per fact learned, tagged **measured** or **doc-quoted**, with date. The guide is written from this. |

## Running

```
go run ./cmd/tools/play_as --record-dir data/stations/cloverfield craftsman-1
```

Inside play_as:

- `station_snapshot`: run at the **start and end of every session**, and at least
  **hourly** while one is running.
- `ledger purchase power_cell 200 bought at Haven`
- `ledger founding credits -5000000`

## Rules

- Never `git add -A` (the tree has dirty agent data). Commit this directory by path.
- Field names come from the recorded raw replies, not from the docs.
- `raw` is only the reply correlated to that exact request (default ws transport). An
  event with no `raw` means no correlated frame arrived (local error, timeout, or
  `--transport mcp`); it is never filled from a previous command.
- Treasury floor 1M; consumable purchases ≤1M total; each facility build is approved first.
