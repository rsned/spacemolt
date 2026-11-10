---
name: reference_shopkeep_consign_overlap_is_fatal
description: "An item in BOTH a worker's shopkeep and consign lists makes bin/worker log.Fatalf at startup — silently (stderr never reaches the overmind log) — and the worker crash-loops to the 100-restart park"
metadata:
  node_type: memory
  type: reference
  originSessionId: 45e19edc-89f8-4358-bb3e-11324020d741
  modified: 2026-11-10T22:27:38.102Z
---

`cmd/worker/main.go` refuses `--shopkeep` and `--consign`/`--consign-offer`
sharing an item ("an item is either sold or consigned, not both") with
`log.Fatalf`. That message goes to the worker's stderr, which the overmind log
does NOT capture: the log shows only `restarting worker "X" (killed=false,
restart #N)` every 5 s, then `parked at the crash-loop cap (100 restarts)`
about 8 minutes later.

**How it bit (2026-11-10):** marketbot_019 got `consign: [nitrogen_ice...]` plus
a `shopkeep: ["nitrogen_ice:..."]` lot (to earn the 1,000 credits that unlock
gifting). SIGHUP relaunched it into a crash loop; parked within 8 min.

**How to apply:** before a mb-fleet.yaml edit, check that no item id appears in
both lists for the same agent. A worker that restarts every 5 s with no output
lines is a startup flag-validation fatal — reproduce by parsing the spec
(worker.ParseShopRules / ParseConsignRules) rather than guessing. SIGHUP after
fixing the yaml relaunches a parked worker (spec change).

Related: [[reference_gift_unlock_and_officer_storage_split]] (the 1,000-earned
credits gift unlock that 019 lacked).
