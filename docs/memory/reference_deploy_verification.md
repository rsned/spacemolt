---
name: reference_deploy_verification
description: "SIGUSR1 drain is time-bounded and ABORTS if workers stay busy — it is not a deploy mechanism. Verify a binary rollout by worker process start time vs binary mtime, never by worker count or overmind_commit."
metadata: 
  node_type: memory
  type: reference
  originSessionId: 2671530b-761a-4dbe-b378-62f725016c20
  modified: 2026-08-01T02:55:04.698Z
---

**Burned 2026-07-31: I reported a deploy as complete when not one worker had restarted.**

**`SIGUSR1` (graceful drain) is NOT a deploy mechanism.** It stops workers as they go idle, and its poll is TIME-BOUNDED. If workers are still mid-mission when it expires the drain simply **aborts and leaves everything running on the old binary**:

```
19:02:02 drain poll ended: 32/38 idle — still busy: [engineer-1 explorer-1 explorer-12
         explorer-2 fighter-10 fighter-2] (force-stop with SIGTERM if needed)
```

Six long-running workers were enough to defeat it. The log says `force-stop with SIGTERM if needed` — believe it.

**To actually roll a worker binary: `kill -TERM` the overmind, wait for workers to exit, `rm -f` the sock, relaunch** (`--stagger 10s`). The TERM cascade is fast — 110 workers exited in **1s** — and workers respawn on whatever `bin/worker` is on disk.

**🔴 Neither of these confirms a rollout, and I used both:**
- **worker COUNT** — 38/38 healthy describes the UNCHANGED workers just as well as new ones.
- **`overmind_commit` in `<fleet>-status.json`** — that is the OVERMIND's build stamp, not the workers'.

**The check that actually works** — every worker's process start time must be LATER than the binary's mtime:
```bash
stat -c %y bin/worker
for d in /proc/[0-9]*; do c=$(tr '\0' ' ' < $d/cmdline 2>/dev/null); \
  case "$c" in bin/worker*<fleet>.sock*) stat -c %y $d | cut -d. -f1;; esac; done | sort | head -1
```
Oldest worker start > binary build time ⇒ rolled. Anything else ⇒ not rolled.

Match on the executable prefix (`bin/worker*`) and scan `/proc/*/cmdline` — `pgrep -f` self-matches the scanning shell. Send signals in a SEPARATE tool call from the scan ([[reference_overmind_launch_commands]] has the kill-signal /proc trap).

Related: [[project_overmind_graceful_drain]] · [[reference_overmind_launch_commands]]
