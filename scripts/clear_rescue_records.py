#!/usr/bin/env python3
"""Archive named rescue records into rescue-history.jsonl and rewrite the queue,
under the same exclusive flock pkg/rescue/queue.go uses.

Usage: clear_rescue_records.py <agent-id> [<agent-id> ...]
"""
import fcntl
import json
import os
import sys
import tempfile

QP = 'data/overmind/rescue-queue.json'
HP = 'data/overmind/rescue-history.jsonl'

targets = set(sys.argv[1:])
if not targets:
    sys.exit('usage: clear_rescue_records.py <agent-id> ...')

lock = open(QP + '.lock', 'a+')
fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
try:
    recs = json.load(open(QP))
    moved = [r for r in recs if r.get('agent_id') in targets]
    keep = [r for r in recs if r.get('agent_id') not in targets]

    with open(HP, 'a') as fh:
        for r in moved:
            fh.write(json.dumps(r, separators=(',', ':')) + '\n')

    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(QP))
    os.close(fd)
    with open(tmp, 'w') as fh:
        json.dump(keep, fh, indent=2)
    os.replace(tmp, QP)

    print('archived: ' + (', '.join(r['agent_id'] for r in moved) or '(none)'))
    print('remaining: ' + (', '.join(r['agent_id'] for r in keep) or '(none)'))
    missing = targets - {r['agent_id'] for r in moved}
    if missing:
        print('NOT FOUND: ' + ', '.join(sorted(missing)))
finally:
    fcntl.flock(lock.fileno(), fcntl.LOCK_UN)
    lock.close()
