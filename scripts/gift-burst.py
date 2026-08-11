#!/usr/bin/env python3
"""Generate a play_as gift-burst script to recapitalize broke agents.

Every line of this exists because a live attempt failed on it first (the
14-agent, 1.4M bailout from johnny_cab on 2026-07-29, and the unlock-pool burst
on 2026-08-11).

MECHANICS THIS ENCODES
  * `recipient` is the GAME USERNAME, not the agent_id, and the two are nothing
    alike: `explorer-4` is "Cosmo 'Cosmic' Chandler", `random-3` is `root`,
    `random-9` is an emoji. Several look truncated at ~24 chars. Transcribing
    them by hand is how the first attempt died -- so this reads them from
    data/agents/<id>/credentials.json and REFUSES to emit a partial script.
  * The SENDER must be DOCKED. All 14 gifts once failed `not_docked` and moved
    zero credits. `autopilot <system> <poi>` travels but does NOT dock, so the
    script always leads with a bare `dock`.
  * The RECIPIENT need not be present or docked. A credit gift is deposited at
    the sender's station and lands in the recipient's wallet; only the sender's
    location matters. (An ITEM gift instead sits in storage and needs
    `withdraw_items` -- this tool sends credits only.)
  * play_as NEVER EXITS ON STDIN EOF -- it spins printing `Error reading input:
    EOF` until your timeout and looks like a hang. Every script ends with `quit`.
  * worker.SplitArgs treats both " and ' as quote chars but copies everything
    verbatim until the matching close, so a double-quoted name keeps its inner
    apostrophes. Names are emitted double-quoted for that reason.

USAGE
    scripts/gift-burst.py --fleet data/overmind/unlock-fleet.yaml
    scripts/gift-burst.py --agents miner-9,prophet-2 --amount 25000
    # a named agent at its own amount, alongside (or instead of) a sweep --
    # --also wins over the sweep for the same agent, so nobody is paid twice
    scripts/gift-burst.py --fleet data/overmind/unlock-fleet.yaml --also databot=1000000

Keep everything in ONE script: a single play_as session handles many commands,
which beats one fresh login per gift against the per-IP /login limit.

Then, because play_as is an interactive REPL needing a real TTY, the OPERATOR
runs it (it cannot be driven from a headless tool shell):

    # 1. free the sender's session if it is supervised -- stopping a
    #    single-agent fleet's overmind beats the SIGSTOP dance, since it
    #    removes the 90s SilenceTimeout pressure entirely
    # 2. go run ./cmd/tools/play_as <sender> < the-generated-script
    # 3. relaunch the overmind

VERIFY FROM THE SENDER, NOT THE RECIPIENTS: each reply carries `credits_sent`,
and get_status shows the wallet plus "Credits gifted"/"Gifts sent" deltas. A
recipient worker's credits in the overmind status file are CACHED and lag badly
-- a gifted agent can read 0 for many minutes. A fresh login re-reads it.
"""

import argparse
import json
import os
import sqlite3
import sys

# Below this an agent cannot reliably re-tank: all-in station fuel runs 2-26
# cr/unit across the galaxy, so one fill of a 150-unit tank can cost ~3,900 at
# the dear end and an agent needs several plus margin.
DEFAULT_FLOOR = 20000
DEFAULT_AMOUNT = 50000


def load_fleet(path):
    import yaml  # optional dep; only needed for --fleet

    with open(path) as fh:
        return [w["agent_id"] for w in (yaml.safe_load(fh) or {}).get("workers", [])]


def credits_for(assets_db, agent_id):
    """Live wallet, preferring the asset ledger and falling back to the agent's
    own checkpoint. Returns (credits, as_of, source); credits is None when the
    agent has never been captured AND has no checkpoint."""
    try:
        row = assets_db.execute(
            "select p.credits, p.captured_at from agents a "
            "join agent_profile p on p.player_id = a.player_id where a.agent_id = ?",
            (agent_id,),
        ).fetchone()
        if row:
            return float(row[0]), row[1][:10], "ledger"
    except sqlite3.Error:
        pass
    # An agent that has belonged to no fleet was never captured, so the ledger
    # has no row at all. Its checkpoint is stale but it is the only record.
    path = os.path.join("data", "agents", agent_id, "checkpoint.db")
    if os.path.exists(path):
        try:
            conn = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
            row = conn.execute(
                "select credits, updated_at from known_state order by id desc limit 1"
            ).fetchone()
            if row:
                return float(row[0]), row[1][:10], "checkpoint"
        except sqlite3.Error:
            pass

    return None, None, "unknown"


def username_for(agent_id):
    path = os.path.join("data", "agents", agent_id, "credentials.json")
    try:
        with open(path) as fh:
            creds = json.load(fh)
    except (OSError, ValueError):
        return None

    return creds.get("username") or creds.get("Username")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    src = ap.add_mutually_exclusive_group(required=True)
    src.add_argument("--fleet", help="overmind fleet YAML; every member is considered")
    src.add_argument("--agents", help="comma-separated agent ids, considered regardless of balance")
    ap.add_argument("--amount", type=int, default=DEFAULT_AMOUNT, help=f"credits per recipient (default {DEFAULT_AMOUNT})")
    ap.add_argument("--floor", type=int, default=DEFAULT_FLOOR, help=f"only gift agents below this balance (default {DEFAULT_FLOOR}); ignored with --agents")
    ap.add_argument("--assets-db", default="data/assets.db")
    ap.add_argument("--out", help="write the play_as script here instead of stdout")
    ap.add_argument(
        "--also",
        action="append",
        default=[],
        metavar="AGENT=AMOUNT",
        help="gift a named agent a specific amount regardless of its balance; repeatable. "
        "Overrides the sweep for that agent so nobody is paid twice.",
    )
    args = ap.parse_args()

    explicit = {}
    for spec in args.also:
        agent_id, _, raw = spec.partition("=")
        if not agent_id or not raw.strip().isdigit():
            print(f"ERROR: --also expects AGENT=AMOUNT with a whole number, got {spec!r}", file=sys.stderr)

            return 1
        explicit[agent_id.strip()] = int(raw)

    if args.fleet:
        candidates, apply_floor = load_fleet(args.fleet), True
    else:
        candidates, apply_floor = [a.strip() for a in args.agents.split(",") if a.strip()], False

    assets = sqlite3.connect(f"file:{args.assets_db}?mode=ro", uri=True)
    # Explicit recipients come last in the candidate list but win on amount, so
    # an agent named in --also is gifted once, at the amount asked for, whether
    # or not the sweep would also have caught it.
    recipients, skipped = [], []
    for agent_id in candidates + [a for a in explicit if a not in candidates]:
        amount = explicit.get(agent_id, args.amount)
        bal, as_of, source = credits_for(assets, agent_id)
        # An unknown balance is treated as broke: the agent has never been
        # captured, which is itself a sign it has been sitting outside every
        # pool, and over-funding costs nothing next to a stranded agent.
        if agent_id not in explicit and apply_floor and bal is not None and bal >= args.floor:
            continue
        name = username_for(agent_id)
        if not name:
            skipped.append(agent_id)
            continue
        recipients.append((agent_id, bal, as_of, source, name, amount))

    if skipped:
        # Refuse rather than emit a short script: a burst that silently drops
        # recipients looks identical to one that covered everybody.
        print(f"ERROR: no username in data/agents/<id>/credentials.json for: {', '.join(skipped)}", file=sys.stderr)

        return 1
    if not recipients:
        print(f"No agent below {args.floor:,} credits; nothing to send.", file=sys.stderr)

        return 0

    width = max(len(r[0]) for r in recipients)
    print(f"{'agent':{width}}  {'credits':>10}  {'as of':10}  {'source':10}  {'gift':>10}  username", file=sys.stderr)
    for agent_id, bal, as_of, source, name, amount in recipients:
        shown = "?" if bal is None else f"{bal:,.0f}"
        flag = " *" if agent_id in explicit else ""
        print(f"{agent_id:{width}}  {shown:>10}  {as_of or '-':10}  {source:10}  {amount:>10,}  {name!r}{flag}", file=sys.stderr)
    total = sum(r[5] for r in recipients)
    print(f"\n{len(recipients)} recipients, {total:,} credits total", file=sys.stderr)
    if explicit:
        print("  * named explicitly via --also", file=sys.stderr)

    lines = ["dock"]
    lines += [f'send_gift "{name}" credits {amount}' for *_, name, amount in recipients]
    lines += ["get_status", "quit"]
    script = "\n".join(lines) + "\n"
    if args.out:
        with open(args.out, "w") as fh:
            fh.write(script)
        print(f"script -> {args.out}", file=sys.stderr)
    else:
        sys.stdout.write(script)

    return 0


if __name__ == "__main__":
    sys.exit(main())
