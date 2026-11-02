---
name: reference_pgrep_self_match_and_sole_member
description: pgrep -f/pkill -f with the target text in your own bash command matches your shell (wrong counts, HUP to the shell, exit 144); a fleet's only member can't be removed via overrides (overmind refuses an empty roster) — use SIGSTOP for a short play_as instead
metadata:
  type: reference
---

**pgrep self-match (2026-11-01):** `pgrep -fc 'bin/worker --agent X '` inside a Bash call counts the Bash process itself, because its command line contains the pattern. That gives the wrong count, and `kill -HUP $(pgrep -f '...' | head -1)` can signal the shell instead of the overmind; `pkill -f line-dashboard` killed the shell (exit 144). Use a bracket pattern that cannot match itself, `pgrep -f '[b]in/worker --agent X '`, or find a server by its port PID with `ss -ltnp`.

**Sole-member fleets:** adding the only agent of a fleet (the_banker in banker) to its overrides `removed` set makes the overmind log `SIGHUP: REFUSING reload — effective roster is EMPTY` and keep running it. For a short play_as on such an agent, SIGSTOP its worker, run play_as, then SIGCONT, per [[reference_sigstop_preserves_game_sessions]].
