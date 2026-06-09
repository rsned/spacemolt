# market-snapshot

Summarize a raw `view_market` response snapshot: how many orders match a given
`source` (e.g. `station`) and the total credits it would cost to buy every unit
of the matching orders.

Unlike `view-market` (which queries the SQLite knowledge base), this tool reads
a single JSON snapshot — the exact payload a `view_market` command returns.

## Usage

```bash
market-snapshot [flags] <market.json>
cat market.json | market-snapshot [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-source` | `station` | Order source to match. Empty string matches all sources (incl. player listings). |
| `-side` | `sell` | `sell` = orders you'd buy from; `buy` = orders you'd sell to. |
| `-top` | `10` | Show the N costliest item types. `0` disables the breakdown. |

## Example

```
$ market-snapshot market.json
Market snapshot: Grand Exchange Station (grand_exchange_station)
Side: sell   Source: station

Orders:         785
Distinct items: 72
Total units:    480,778,732
Total cost to buy all: 561,157,587,543 cr

Top 10 item types by cost:
  item                                 orders            units                 cost
  shield_emitter                           12      222,090,159      132,737,225,974
  ...
```
