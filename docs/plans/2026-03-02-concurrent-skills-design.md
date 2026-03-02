# Concurrent Skills Design: Primary + Background Skill Execution

**Date:** 2026-03-02
**Status:** Approved

## Problem

Agents running long-duration primary skills (e.g., `scan_for_distress`) spend most of their time idle — sleeping between scan cycles. This wastes game ticks that could be used for productive work like mining, harvesting, or crafting.

## Solution

A composite skill execution model where a **primary skill** declares idle windows and a **background skill** fills those windows with productive work. When the primary needs control back, the background gracefully checkpoints, cleans up, and yields.

## Design Decisions

- **1 primary + 1 background** (strictly, no tertiary)
- **Graceful checkpoint** interrupt policy: background saves state, runs cleanup (return to station + dock), then yields
- **Hybrid YAML + Agent Config**: skill YAML declares a `background_slot` (the contract), agent personality YAML fills in which skill to run

## YAML Schema Changes

### Skill YAML: `background_slot` (optional)

```yaml
name: scan_for_distress
description: ...

background_slot:
  description: "Runs during 5-minute scan sleep intervals"
  interrupt: graceful          # graceful | immediate | abandon
  cleanup_outputs: [docked]    # state the background must restore before yielding
  idle_steps: [sleep_cycle]    # which primary steps represent idle time

steps:
  # ... existing steps unchanged
```

The skill does NOT name which background skill runs — it only declares the slot and its contract.

### Agent Personality YAML

```yaml
skills:
  primary: scan_for_distress
  background: mine    # fills the background_slot
```

Different agents can fill the slot with different skills (`mine`, `craft_items`, etc.) or leave it empty to just idle.

## Checkpoint Data Structure

```go
type SkillCheckpoint struct {
    SkillName    string            // "mine"
    CurrentStep  string            // "mine_loop"
    StepState    map[string]any    // e.g., {"cargo_pct": 0.52, "mining_site": "belt-42"}
    Interrupted  bool
    CleanupDone  bool
}
```

## Interrupt Lifecycle

```
PRIMARY enters idle_step (e.g., sleep_cycle)
  └─► BACKGROUND starts (or resumes from checkpoint)
        └─► Background runs skill normally (mine, travel, etc.)

PRIMARY exits idle_step (timer expires OR distress found)
  └─► Signal BACKGROUND to interrupt
        └─► Background saves checkpoint {current_step, state}
        └─► Background runs cleanup (return_to_station → dock → deposit)
        └─► Background yields control
              └─► PRIMARY continues (scan_chats, assist_deliver, etc.)

PRIMARY re-enters idle_step
  └─► BACKGROUND resumes from checkpoint (or starts fresh if completed)
```

**Ordering guarantee:** Only one skill sends commands to the game client at a time. The composite strategy serializes access — primary owns the connection during active steps, background owns it during idle steps, handoff is explicit.

## Go Architecture

### New Files

| File | Purpose |
|------|---------|
| `pkg/strategy/composite.go` | `CompositeStrategy` — orchestrates primary + background with checkpoint/interrupt |
| `pkg/strategy/checkpoint.go` | `SkillCheckpoint` type, serialization, save/restore |
| `pkg/strategy/background.go` | `BackgroundRunner` — wraps a skill with interrupt channel and cleanup logic |

### CompositeStrategy

Implements the existing `Strategy` interface so it drops into the current runner infrastructure with no changes to `Runner`, `Manager`, or `auto-*` binaries.

```go
type CompositeStrategy struct {
    primary    Strategy
    background Strategy

    // From YAML background_slot
    interruptPolicy string        // "graceful"
    cleanupOutputs  []string      // ["docked"]
    idleSteps       []string      // ["sleep_cycle"]

    // Runtime state
    checkpoint *SkillCheckpoint
    bgCancel   context.CancelFunc
    mu         sync.Mutex
}
```

### Idle Signaling

Channel-based: the primary's sleep step blocks on a `select` that fires either when the timer expires OR when a distress is found. The composite strategy hooks into this via `onIdleEnter()`/`onIdleExit()` callbacks.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Background skill fails mid-execution | Log error, clear checkpoint, primary unaffected. Background starts fresh next idle window. |
| Cleanup fails (can't dock in time) | 2-minute timeout on cleanup. Force-abandon background state, let primary take over. |
| Idle window too short | `min_idle_duration` config (e.g., 60s). Skip starting background if window is shorter. |
| Player dies during background | Treat as interrupt. Checkpoint becomes invalid (new location), clear and start fresh. |
| Background completes naturally | Clears checkpoint, can start a fresh cycle if idle time remains. |

## Example Flow

```
T+0:00  PRIMARY: check_announce → announce_services
T+0:10  PRIMARY: scan_chats → no distress found
T+0:20  PRIMARY: enters sleep_cycle (5 min idle)
T+0:20  BACKGROUND: starts mine skill (fresh)
T+0:20  BACKGROUND: undock → travel to belt → mine loop
T+3:30  BACKGROUND: cargo full → return → dock → deposit (done, checkpoint cleared)
T+4:00  BACKGROUND: fresh mine cycle starts
T+5:20  PRIMARY: sleep_cycle expires → signal interrupt
T+5:20  BACKGROUND: saves checkpoint {step: mine_loop, cargo_pct: 0.35}
T+5:20  BACKGROUND: cleanup → return → dock → deposit
T+5:50  BACKGROUND: yields
T+5:50  PRIMARY: scan_chats → FOUND distress!
T+5:50  PRIMARY: assist_deliver (buy, travel, jettison, notify, return, dock)
T+8:10  PRIMARY: enters sleep_cycle again
T+8:10  BACKGROUND: resume from checkpoint → undock → travel → mine
```
