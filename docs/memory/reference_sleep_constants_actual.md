---
name: reference_sleep_constants_actual
description: "Actual pkg/game/constants.go Sleep values differ from the CLAUDE.md table — verify the file, don't trust the doc"
metadata: 
  node_type: memory
  type: reference
  originSessionId: 696d1b09-e994-42b5-8a6c-d8be5aa101d8
---

The Sleep-constants table in both `CLAUDE.md` files is STALE for several values. The authoritative source is `pkg/game/constants.go` (verified 2026-06-20):

| Constant | Actual (code) | CLAUDE.md says |
|----------|---------------|----------------|
| SleepTick | 10s | 10s ✓ |
| SleepQuick | 2s (Tick/5) | 2s ✓ |
| SleepShort | ~3.3s (Tick/3) | 5s ✗ |
| SleepMedium | 5s (Tick/2) | 30s ✗ |
| SleepLong | 20s (2·Tick) | 60s ✗ |
| SleepDock | 15s | 15s ✓ |
| SleepReconnect | 30s | 30s ✓ |

**Why it matters:** when picking a Sleep constant for timing, compute the real magnitude from the code, not the doc. This bit the overmind Plan A work — the plan assumed `SleepMedium=30s` for the supervisor reap/status tickers, but it's actually 5s (chattier than intended; not broken), and `5·SleepLong` was 100s not the 5 min intended (fixed to `30·SleepTick`).

**How to apply:** before using a Sleep constant for a threshold/interval, read `pkg/game/constants.go`. Consider proposing a one-line fix to the CLAUDE.md tables (Short/Medium/Long) — not yet done.
