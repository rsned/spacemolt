#!/usr/bin/env python3
"""Retune capture_action_log from hourly to twice_daily across all agents.

MUST run with the fleet STOPPED. A live worker holds its schedule in memory and
rewrites schedule.json on every fire (Scheduler.checkDue -> saveLocked), so an
edit applied underneath it is silently reverted within minutes.

Why a script and not the roles.yaml seed: seeding is covered-aware, and
Covers("hourly","twice_daily") is true (12h is a multiple of 1h), so declaring
the coarser cadence beside the finer one is a no-op -- the hourly entry has to
be rewritten in place. Idempotent: re-running it changes nothing.
"""
import glob
import json
import sys

DRY = "--apply" not in sys.argv
changed = skipped = 0

for path in sorted(glob.glob("data/agents/*/schedule.json")):
    try:
        with open(path) as fh:
            doc = json.load(fh)
    except (OSError, json.JSONDecodeError) as exc:
        print(f"  SKIP {path}: {exc}")
        continue

    tasks = doc if isinstance(doc, list) else doc.get("tasks", [])
    hit = False
    for task in tasks:
        if task.get("command") != "capture_action_log":
            continue
        if task.get("frequency") == "hourly":
            task["frequency"] = "twice_daily"
            hit = True
        elif task.get("frequency") == "twice_daily":
            skipped += 1

    if hit:
        changed += 1
        if not DRY:
            with open(path, "w") as fh:
                json.dump(doc, fh, indent=2)
                fh.write("\n")

print(f"{'DRY RUN -- ' if DRY else ''}{changed} agent(s) hourly -> twice_daily"
      f"{f', {skipped} already twice_daily' if skipped else ''}")
if DRY:
    print("re-run with --apply (fleet must be stopped)")
