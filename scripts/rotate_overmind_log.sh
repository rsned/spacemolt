#!/usr/bin/env bash
# Rotate overmind logs in place, without restarting the overmind.
#
# The overminds hold their log open in append mode (launched with >>), so the
# file can be truncated under them: the next write lands at the new end. Each
# log is compressed to data/overmind/log-archive/<name>.<utc>.zst, the lines
# written while compressing are carried over, and the live file is cut down to
# those lines.
#
# Usage: scripts/rotate_overmind_log.sh data/overmind/mb-overmind.log [more.log ...]
set -euo pipefail

for log in "$@"; do
  [ -f "$log" ] || { echo "skip $log: not a file" >&2; continue; }
  dir=$(dirname "$log")/log-archive
  mkdir -p "$dir"
  out="$dir/$(basename "$log" .log).$(date -u +%Y%m%dT%H%M%SZ).log.zst"
  size=$(stat -c %s "$log")
  head -c "$size" "$log" | zstd -T0 -3 -q -o "$out"
  # Verify the archive before cutting anything.
  zstd -t -q "$out"
  tail -c "+$((size + 1))" "$log" > "$log.carry"
  truncate -s 0 "$log"
  cat "$log.carry" >> "$log"
  rm -f "$log.carry"
  echo "rotated $log: $((size / 1000000)) MB -> $out ($(($(stat -c %s "$out") / 1000000)) MB)"
done
