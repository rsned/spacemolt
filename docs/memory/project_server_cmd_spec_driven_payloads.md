---
name: project_server_cmd_spec_driven_payloads
description: "Deferred task: route cmd/tools/server-cmd through the typed pkg/game client wrappers that play_as already uses, instead of guessing payload types from string syntax"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2cf71781-6ccf-44b8-b879-8971d8d06726
  modified: 2026-07-27T02:31:01.318Z
---

**Operator direction, 2026-07-26:** *"the longer term fix for server-cmd is to ensure it matches the latest commands spec format (field names and types)… it should look up loot_wreck in the spec or use one of our library wrappers."*

**Why:** `cmd/tools/server-cmd` was last touched **2026-03-10** and guesses payload types from string syntax. That produced a genuinely nasty bug — `parseValue`'s float branch used `fmt.Sscanf("%f")`, which parses a PREFIX without error, so `wreck_id=158222749cdf...` was sent as the number `158222749` and the server answered `not_found` for a wreck that plainly existed. Point fix landed (strict `strconv.ParseInt`/`ParseFloat`), but the underlying design still guesses. [[reference_jettison_loot_transfer_flow]]

**Still broken by design after the point fix:** an identifier composed ENTIRELY of digits is still coerced to a number, because nothing in `key=value` says which keys are ids. Only the spec knows.

**⭐ THE ACTUAL FIX — reuse play_as, do NOT reinvent this** (operator, 2026-07-26: *"play_as has had to solve most of this already"*). `cmd/tools/play_as` never guesses types. Each command is an explicit case that validates arity, parses only the genuinely-numeric args (`parseQuantity`), and then calls a **typed `pkg/game` client wrapper** — the Go signature is the type contract, so field names and JSON types are fixed by the client library:

```go
case "loot", "loot_wreck":
    if len(parts) < 4 { return fmt.Errorf("usage: loot <wreck-id> <item-id> <quantity>") }
    qty, err := parseQuantity(parts[3])
    ...
    return client.LootWreck(ctx, parts[1], strings.ToLower(parts[2]), qty)
```

So server-cmd should route through those same wrappers (`client.LootWreck`, `client.SalvageWreck`, `client.TowWreck`, …) for every command that has one, and keep raw `--payload` only as the escape hatch for commands that don't — with the blind numeric coercion gone either way. The clean version extracts play_as's per-command arg→wrapper dispatch into a package both tools import; the obstacle is that the dispatch is a ~10k-line switch in `play_as/main.go` tangled with output formatting, so extraction is the real work here, not the type logic.

**Spec cross-check (secondary):** `server_docs/openapi.json` also declares every command's request body with exact field names and JSON types (e.g. `/loot_wreck` → `wreck_id: string`, `item_id: string`, `quantity: integer minimum 1`), it is symlinked to the current snapshot, and `pkg/actionspace`'s `TestLoadFromOpenAPIContainsAllHardcoded` already guards it against drift. So:
1. Coerce a raw `--payload` value only to its **declared** type, leaving anything typed `string` alone.
2. **Reject unknown field names** and report the valid set — catches typos that currently sail through as ignored payload keys.

**Also worth adding:** server-cmd sends no `request_id`. Per the operator it is **optional** — added so server push messages can be paired with request responses when things arrive mid call/response — so this is a nicety, not a defect. Populating it would make `--debug` output easier to correlate.

**Debugging note that made this findable:** `server-cmd --debug` dumps the exact wire payload both directions. It settled in one run what six behavioural experiments could not. Use it before theorising about game rules.
