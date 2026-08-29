---
name: project_haul_fleet_worker_update_due
description: "Haul fleet (21 workers) is running an old bin/worker and needs a drain+relaunch onto the current build — operator-requested 2026-07-26, not yet done"
metadata: 
  node_type: memory
  type: project
  originSessionId: 2f3b8937-e63d-42aa-8015-c67d52bc5fd2
  modified: 2026-07-26T17:26:15.384Z
---

**The haul fleet is behind on `bin/worker` and the operator wants it updated** (asked 2026-07-26, NOT yet done). It missed the v0.2.3 log-tagging / v0.2.4 tier-logging roll and everything since.

Haul does NOT carry freight (`enable_freight` appears only in `mission-learn-fleet.yaml`), so none of the freight fixes change its behaviour — this is about log tagging, version visibility on the dashboard, and not drifting further behind.

Procedure is the standard one, with the haul-specific trap: **`--stagger 10s` is mandatory** for the 21-worker fleet or the per-IP `/login` limit strands ~12 workers with restarts=0 and no retry. Full relaunch line in [[reference_overmind_launch_commands]].

```
kill -USR1 <haul-overmind-pid>     # drain; poll is bounded ~3.5min
# wait for "drain poll ended: N/N idle" in data/overmind/haul-overmind.log
kill -TERM <pid>; rm -f data/overmind/haul.sock
./bin/overmind --socket data/overmind/haul.sock --fleet data/overmind/haul-fleet.yaml --stagger 10s
```

**Check the arbitrage-scanner at the same time** — it is unsupervised, the haul fleet earns nothing without it, and a past relaunch missed it for 10h. [[reference_haul_fleet_capacity_ceiling]] [[project_scanner_outage_expiry_fix]]
