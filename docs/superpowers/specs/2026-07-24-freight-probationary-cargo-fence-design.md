# Freight Probationary-Cargo Fence Design

**Date:** 2026-07-24
**Status:** Approved

## Context

The freight-probation bootstrap (v0.2.0) lets a *probationary* carrier accept
loss-leader hauls (relaxed net floor −400) so it accumulates the
`successful_deliveries` + `delivered_value` needed to advance to the ~4x-paying
licensed tier. Probationary-band freight is scarce on the boards, so those hauls
are the bottleneck resource for every carrier still climbing.

Live-fleet observation (2026-07-24): the current selection is tier-blind. The 500
net floor that applies to an above-probationary carrier already rejects
*loss-leader* probationary cargo, but a *profitable* probationary contract
(net ≥ 500) is still grabbed by whatever carrier reaches it first — including
carriers that have already advanced past probationary and no longer need it.
That steals an advancement opportunity from an agent still climbing.

This design fences above-probationary carriers off probationary-band cargo so
the scarce probationary hauls are reserved for the agents that need them to
advance. Advanced carriers keep working their own-tier and higher freight
(the whole point of climbing), and probationary carriers are untouched.

## Goal

An above-probationary carrier never accepts a probationary-band freight contract
while the bootstrap policy is active, leaving those contracts for carriers still
in the probationary tier. Higher-band cargo and probationary carriers are
unaffected.

## Scope decisions (settled during brainstorming)

- **Advanced policy: skip only probationary cargo.** An above-probationary
  carrier still accepts licensed/trusted/prime cargo it qualifies for; it does
  NOT drop freight entirely. Higher-tier freight pays ~4x and is the reason to
  climb.
- **Rule scope: probationary boundary only.** The fence applies only to
  probationary-band cargo. Licensed/trusted carriers still compete for each
  other's tiers (not modeled — YAGNI; the observed contention is at the
  probationary boundary).
- **Gating: reuse `FreightBootstrap`.** The bootstrap flag already means
  "help probationary agents climb"; the fence is the other half of that intent
  (don't let climbed agents steal the climbers' hauls). One switch, no new flag,
  already default-on wherever `EnableFreight` is set.

## Architecture

A tier-blind selection becomes tier-aware via one filter in the existing
per-listing loop of `freightCandidate` (`pkg/worker/freight.go`). Two pure
helpers decide the filter; the loop drops fenced listings before
`selectFreightCand` scores them. No new server calls: `ShipmentContract.RiskBand`
(the contract's required carrier tier, values
`probationary|licensed|trusted|prime`, plus `unpriced`) and the carrier's own
`prof.Progression.CurrentTier` are already present in the board/profile responses
`freightCandidate` fetches.

## Components

### Helper: `carrierTierAboveProbationary`

```go
// carrierTierAboveProbationary reports whether a KNOWN tier outranks
// probationary. An empty/unknown tier returns false: never fence a carrier that
// might itself be probationary (that would starve it of the hauls it needs).
func carrierTierAboveProbationary(tier string) bool {
    return tier != "" && tier != carrierTierProbationary
}
```

### Helper: `freightBandExcluded`

```go
// freightBandExcluded reports whether to skip a board listing to reserve
// probationary-band cargo for carriers still climbing out of the probationary
// tier. Gated on the bootstrap switch (the fleet-wide "help the probationary
// climb" policy): with bootstrap off, selection is unchanged. Pure — the caller
// supplies the live carrier tier, the contract's required band, and the toggle.
func freightBandExcluded(carrierTier, contractBand string, bootstrapEnabled bool) bool {
    return bootstrapEnabled &&
        contractBand == carrierTierProbationary &&
        carrierTierAboveProbationary(carrierTier)
}
```

`carrierTierProbationary` already exists in `freight.go` (value `"probationary"`,
matching `ShippingProfileResponse.Progression.CurrentTier` and
`ShipmentContract.RiskBand`).

### Filter in `freightCandidate`

Inside the existing `for _, l := range board.Shipments` loop, as the **first**
check (before the aggregate-liability check and `HopsTo` route resolution, so a
fenced contract costs no route lookup):

```go
if freightBandExcluded(prof.Progression.CurrentTier, l.Contract.RiskBand, deps.FreightBootstrap) {
    fmt.Fprintf(out, "freight: skip %s: probationary cargo reserved for climbing carriers (carrier tier %s)\n",
        l.Contract.ID, prof.Progression.CurrentTier) //nolint:errcheck
    continue
}
```

The skip is logged with a distinct reason so the freight log shows *why* an
advanced carrier passed on probationary cargo, mirroring the existing per-skip
logging in the loop.

## Behavior table

| Carrier tier | Bootstrap | Probationary-band contract | Higher-band contract |
|---|---|---|---|
| probationary | on | take (−400 floor, unchanged) | ineligible (server-marked) |
| licensed / trusted / prime | on | **skip (fenced)** | take, 500 floor (unchanged) |
| any | off | take (unchanged) | take (unchanged) |
| unknown / empty | on | take (safe default — not fenced) | take |

## Interaction with the existing net floor

Complementary, not redundant:

- The 500 net floor (`effectiveFreightFloor` at a non-probationary tier) already
  rejects *loss-leader* probationary cargo (net < 500) for an advanced carrier.
- The fence additionally rejects a *profitable* probationary contract
  (net ≥ 500) that the floor would let through — the observed leak.
- A probationary carrier: `carrierTierAboveProbationary` is false, so the fence
  never fires; it keeps taking probationary cargo on the relaxed −400 floor.
  Unchanged.

## Edge cases

- **`RiskBand` is `"unpriced"` or `""`** → not equal to `"probationary"` → never
  fenced.
- **Unknown/empty carrier tier** → `carrierTierAboveProbationary` false → not
  fenced (never starve a possibly-probationary carrier of its own hauls). This
  mirrors how `effectiveFreightFloor` treats an unknown tier conservatively
  rather than granting it special probationary handling.
- **Advanced carrier at a station whose only eligible cargo is
  probationary-band** → every listing fenced → `cands` empty →
  `selectFreightCand` returns nil → existing `"no freight cleared the gate"`
  path → the pass falls through to missions. Intended outcome.

## Testing

`pkg/worker/freight_test.go` (follows existing table-test + fake-board patterns):

1. **`carrierTierAboveProbationary` truth table** — `""`→false,
   `"probationary"`→false, `"licensed"`/`"trusted"`/`"prime"`→true.
2. **`freightBandExcluded` truth table** — the four rows of the behavior table:
   probationary carrier never fenced; advanced carrier fenced only for
   probationary band; bootstrap-off never fences; unknown tier never fenced;
   a `"licensed"`-band contract never fenced regardless of carrier.
3. **`freightCandidate` — advanced carrier fences probationary, keeps higher.**
   Licensed carrier, board = probationary(net 800) + licensed(net 600);
   bootstrap on → selects the licensed contract, log contains the fence-skip line
   for the probationary contract.
4. **`freightCandidate` — probationary carrier unaffected.** Probationary
   carrier, same board → selects the probationary contract (highest net 800),
   no fence-skip logged.
5. **`freightCandidate` — bootstrap off disables the fence.** Licensed carrier,
   same board, bootstrap off → selects the probationary contract (highest net),
   no fence.
6. **`freightCandidate` — advanced carrier, probationary-only board falls
   through.** Licensed carrier, board = probationary only; bootstrap on → returns
   nil candidate with the `"no freight cleared the gate"` reason.

## Constraints

- `golangci-lint` clean, no new findings.
- Any sleeps/pauses use `pkg/game/constants.go` constants (none expected here).
- No new struct fields, flags, or server calls; reuses `deps.FreightBootstrap`,
  `prof.Progression.CurrentTier`, and `l.Contract.RiskBand`.
- All existing `pkg/worker` tests stay green.
- Behavior is inert unless `EnableFreight` (freight path runs at all) AND
  `FreightBootstrap` (fence gated on it) are both set.

## Out of scope

- Symmetric fencing at higher tier boundaries (trusted carrier skipping licensed
  cargo). Deferred as YAGNI; revisit only if higher-tier contention is observed.
- Repositioning carriers off freight-dead stations (player-owned/border/customs
  stations that post no probationary freight). A separate positioning concern;
  not a selection change.
- Marketbot freight-demand scan / per-tier certification rotation. Tracked
  separately.
