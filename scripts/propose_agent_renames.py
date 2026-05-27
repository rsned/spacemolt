#!/usr/bin/env python3
"""Propose new usernames for all agents in data/agents/.

Rules:
- For "First 'Nickname' Last" patterns: drop the quoted nickname, use "First Last".
- If credentials.json username is truncated (often at 24 chars), recover the full
  last name from personality.json's `name` field.
- Unicode-only pirate names (pirate-1..5) and emoji-only random names (random-8/9)
  get hand-picked proper names that fit the biography.
- Service/system accounts (assist-*, databot, overmind, architect-*, spark-*,
  empire-*, assist-*, root/superuser/Agent McAgentFace style) are left alone.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

AGENT_DIR = Path("/home/robert/spacemolt/spacemolt/data/agents")
MAX_LEN = 24

# Hand-picked replacements for agents with no usable text name.
HAND_PICKED: dict[str, str] = {
    "pirate-1": "Redbeard Cross",
    "pirate-2": "Calico Jack Hollis",
    "pirate-3": "Bartholomew Barnacle",
    "pirate-4": "Karkoa Kaine",
    "pirate-5": "Vera Snake-eyes",
    "random-8": "Specs Mendoza",
    "random-9": "Rocker Vargas",
}

# Agents to skip entirely (service accounts, NPCs, etc).
SKIP = {
    "assist-frontier", "assist-haven", "assist-krynn", "assist-nexus", "assist-sol",
    "databot", "overmind", "architect-1", "architect-2", "architect-3", "architect-4",
    "architect-5", "spark-1", "spark-2", "spark-3", "spark-4", "spark-5",
    "empire-crimson", "empire-crimson-real",
    "random-2", "random-3",  # superuser / root
    "random-6", "random-7",  # Agent McAgentFace
}

NICKNAME_RE = re.compile(r"^(?P<first>.+?)\s+'(?P<nick>[^']+)'\s+(?P<last>.+)$")


def propose(agent_id: str, current: str, full_name: str | None) -> str | None:
    """Return a proposed username, or None to skip."""
    if agent_id in SKIP:
        return None
    if agent_id in HAND_PICKED:
        return HAND_PICKED[agent_id]

    source = full_name or current
    m = NICKNAME_RE.match(source)
    if m:
        proposed = f"{m['first']} {m['last']}".strip()
    else:
        # No quoted nickname — leave as-is unless current is truncated.
        if full_name and len(current) >= MAX_LEN and full_name != current:
            proposed = full_name
        else:
            proposed = current

    if len(proposed) > MAX_LEN:
        proposed = proposed[:MAX_LEN].rstrip()
    return proposed


def main() -> int:
    rows: list[tuple[str, str, str, str]] = []
    for d in sorted(AGENT_DIR.iterdir()):
        if not d.is_dir():
            continue
        cred = d / "credentials.json"
        pers = d / "personality.json"
        if not cred.exists() or not pers.exists():
            continue
        cred_data = json.loads(cred.read_text())
        pers_data = json.loads(pers.read_text())
        current = cred_data.get("username", "")
        full_name = pers_data.get("name")
        proposed = propose(d.name, current, full_name)
        if proposed is None or proposed == current:
            continue
        rows.append((d.name, current, proposed, full_name or ""))

    w = (
        max(len(r[0]) for r in rows) if rows else 8,
        max(len(r[1]) for r in rows) if rows else 8,
        max(len(r[2]) for r in rows) if rows else 8,
    )
    print(f"{'agent_id':<{w[0]}}  {'current_username':<{w[1]}}  {'proposed_username':<{w[2]}}  personality_name")
    print("-" * (w[0] + w[1] + w[2] + 40))
    for r in rows:
        print(f"{r[0]:<{w[0]}}  {r[1]:<{w[1]}}  {r[2]:<{w[2]}}  {r[3]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
