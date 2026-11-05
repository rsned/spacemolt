#!/bin/bash
# storage-item-watch.sh — append a CRFT faction-storage item's quantity to a log
# whenever its capture changes, so a slow consumption rate (one unit per
# maintenance cycle) can be read off as a history. The asset ledger keeps only
# the latest row per item; this keeps the sequence.
#
# Usage: scripts/storage-item-watch.sh <item_id> <log> [interval_seconds] [base_id]
# Read-only against data/assets.db. Run under setsid nohup; stop with kill.
set -u
item=${1:?item_id}
log=${2:?log path}
interval=${3:-900}
base=${4:-c976fa7df53c87f9616165d7d411b41a}   # Conjunction Junction
db=data/assets.db
last=""
while :; do
  row=$(sqlite3 -readonly "$db" "PRAGMA busy_timeout=5000;
        select captured_at || ' ' || cast(quantity as integer) from faction_storage_items
         where base_id='$base' and item_id='$item';" 2>/dev/null | tail -1)
  if [ -n "$row" ] && [ "$row" != "$last" ]; then
    echo "$(date -u +%Y-%m-%dT%H:%MZ)  $item  captured=${row% *}  qty=${row##* }" >> "$log"
    last=$row
  fi
  sleep "$interval"
done
