---
name: feedback_play_as_go_run
description: "User always runs play_as via `go run ./cmd/tools/play_as` (fresh build), not the bin/ binary"
metadata: 
  node_type: memory
  type: feedback
  originSessionId: 868ac572-f106-4923-9823-e82622bf66b9
---

The user always launches the play_as REPL with `go run ./cmd/tools/play_as <agent-id>`, which compiles from current source each time — NOT the checked-in `bin/play_as` (which can be stale).

**Why:** guarantees they're running the latest command set / fixes without a manual rebuild step.

**How to apply:** don't warn about `bin/play_as` being out of date when suggesting play_as commands to the user — their invocation is always current. When giving play_as command syntax, just give the in-REPL command (e.g. `send_gift <recipient> credits <amount>`); they'll run it via `go run`.

Note: play_as is an interactive `liner` REPL needing a real TTY, so it can only be driven by the user, not from a headless tool shell. To issue a manual command AS an overmind-managed worker agent (which holds the game session), the worker must first be stopped and the supervisor frozen so it can't respawn — see the salvager-2→trader-8 gift procedure. Non-worker agents (e.g. craftsman-1) have no session contention and the user can play_as them freely. [[reference_overmind_launch_commands]]
