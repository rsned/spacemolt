# Hunt refit and ammo depot

Operator steps for the decision-capture hunt rollout. The code is in `pkg/worker` (`hunt.go`, `hunt_sortie.go`, `hunt_rearm.go`) and `pkg/huntdecide`. The design is `docs/superpowers/specs/2026-11-21-hunt-decision-capture-design.md`.

Nothing here is automated. Refit, depot fill and restart are all hand steps.

## Background

Each hunter keeps its ammo in its own storage at its home station, which is the `station` field in `data/overmind/hunt-fleet.yaml` (`frontier_station`). Before a sortie it brings rounds aboard from that depot, then from the station market under a credit cap. When it runs dry it returns to the nearer of its depot and a policed station that sells its ammo.

A hunter never leaves dock for a job with no weapon able to fire. If it is unarmed at the depot and the market cannot arm it, the sortie ends `blocked_unarmed` and the worker reports a failed outcome under `behaviour:hunt_sortie`.

Provisioning treats one ammo item as one reload, for every ammo type. This is confirmed in the item catalog for autocannon boxes and railgun cases ("one box fully loads any autocannon magazine"). It is not confirmed for missiles. See the missile check below before fitting launchers.

## Proposed loadout

This is a proposal. Approve the final mix after the live checks in the next section.

| Hunter | Weapons | What it tests |
|---|---|---|
| pirate-5 | 3x pulse_laser_ii | energy, no ammo, reach 3 |
| pirate-9 | 3x autocannon_ii | kinetic, big magazine, reach 2 |
| pirate-10 | 2x railgun_i + pulse_laser_i | kinetic, 5-round magazine, reach 5 |
| pirate-13 | 2x missile_launcher_ii + pulse_laser_i | explosive, reach 6; launchers are at nova_terra_central |
| pirate-6 | autocannon_ii + railgun_i + pulse_laser_i | mixed reach on one ship |
| pirate-7 | pulse_laser_i | single-weapon baseline, no ammo |
| pirate-8 | autocannon_ii | single-weapon baseline, ammo |

Hulls today: five Gauntlets (3 weapon slots, 22 CPU, 50 power: pirate-5, -6, -9, -10, -13), two Prospectors (1 weapon slot: pirate-7, -8), and pirate-1 quarantined in a Sparrow.

Ammo at `frontier_station`: standard rounds (tens of thousands), ferrous slugs (675), guided missiles (101). Most of the stock is in craftsman-1's storage.

## Live checks before fitting

Read each hunter's current state through `play_as`. Run it with `go run`, never from `bin/`.

```
go run ./cmd/tools/play_as pirate-9
```

For every hull, check:

1. The modules fitted now, and which slots are free.
2. On a Gauntlet, the CPU budget of 22 and the power budget of 50. Add up the CPU and power of the three weapons and of whatever else is fitted. Three autocannon_ii or two missile_launcher_ii plus a laser must fit inside both numbers.
3. That the hunter is docked at `frontier_station` and the modules you want are in its storage there, or at nova_terra_central for the missile launchers.

If a mix does not fit the budget, drop the hunter to a smaller mix and say so in the table before you fit.

## Missile check before pirate-13

Do this before putting any hunter on missiles. One ammo item may not fill a launcher magazine: launcher magazines hold 8 and 12, and the catalog text is silent on how many missiles one item loads.

1. Fit one missile_launcher_ii on pirate-13.
2. Put `standard_guided_missiles` aboard.
3. Run a `reload` through `play_as` and read `current_ammo` against `magazine_size` in the reply.

```
go run ./cmd/tools/play_as pirate-13
```

If one item fills the magazine, go ahead with the fit. If one item does not fill it, do not put that hunter on missiles. Provisioning would count the launcher as stocked while the magazine stays short. Wait until a per-ammo-type factor is added to provisioning.

## Fill the depot

Stock each hunter's own storage at `frontier_station` from craftsman-1's storage there. The minimum is 4 sorties' worth, which is the number of ammo-fed weapons times 2 reloads times 4.

| Hunter | Ammo-fed weapons | Minimum items |
|---|---|---|
| pirate-9 | 3 | 24 |
| pirate-10 | 2 | 16 |
| pirate-13 | 2 | 16 |
| pirate-6 | 2 | 16 |
| pirate-8 | 1 | 8 |

pirate-5 and pirate-7 fire no ammo and need no depot stock. Use the right item for each weapon: standard rounds for autocannons, ferrous slugs for railguns, guided missiles for launchers.

Move the stock with `play_as` as craftsman-1 (withdraw to cargo, then transfer into the hunter's storage), or gift it to the hunter. A gift lands in the hunter's own storage. Check the stock afterward by reading the hunter's storage at `frontier_station`.

A hunter reports one line when its depot falls below 2 sorties' worth. Refill when you see it.

## Fund pirate-6

pirate-6 holds 3 credits and 3 fuel. It cannot buy fuel or ammo from the market until it is funded. Gift it credits and fuel, or gift it ammo stock directly. The credit floor kept for fuel and repair means a hunter with almost no credits will not buy ammo at all.

## Flags and fleet yaml

Two optional agent fields, forwarded to the worker only when set:

| Yaml field | Worker flag | Default |
|---|---|---|
| `hunt_ammo_cap` | `--hunt-ammo-cap` | 5000 credits per sortie |
| `hunt_explore` | `--hunt-explore` | empty, exploration off |

Leave both unset for this rollout. Exploration is off for every decision kind. A bad `--hunt-explore` value stops the worker at startup. The error goes to stderr and to the worker log.

## Rollout order

Build the worker into `bin/`.

```
go build -o bin/worker ./cmd/worker
```

1. Restart one hunter first. Pick one with an ammo-free or simple loadout, such as pirate-7 or pirate-8. Follow the cold start runbook for how to restart a worker. Do not restart a disconnected worker by hand; only the gate reconnects it.
2. Watch one full sortie. Read its records as it goes (see below) and the worker log.
3. Check `data/agents/<agent>/task-outcomes.json` for the key `behaviour:hunt_sortie`. `failed` means `blocked_unarmed`, `stranded` or `destroyed`. Any other result is `ok`, with the result in the reason.
4. If the records look right, restart the rest.

## Read a day of records

Records are JSON lines, one file per agent per day, under `data/agents/<agent>/hunt-decisions/`. They are git-ignored.

Sortie results for one hunter and day:

```
jq -c 'select(.type=="sortie_end")' data/agents/pirate-9/hunt-decisions/2026-11-21.jsonl
```

Decisions with the rule's pick and the option actually chosen:

```
jq -c 'select(.type=="decision") | {seq, kind, chosen: .option_id, rule: .rule_option_id, source}' data/agents/pirate-9/hunt-decisions/2026-11-21.jsonl
```

Every decision should show `source` as `rule` while exploration is off.
