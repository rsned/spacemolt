#!/usr/bin/env bash
# Usage: fit-miningbots.sh <ice|gas> <bot-id>...
# Pipes data/scripts/fit_<group>.txt into play_as for each bot.
set -u
cd "$(dirname "$0")/.."
if [ $# -lt 2 ]; then
  echo "usage: $0 <ice|gas> <bot-id>..." >&2
  exit 2
fi
group=$1
shift
file="data/scripts/fit_${group}.txt"
[ -f "$file" ] || { echo "missing $file" >&2; exit 2; }
failed=()
for bot in "$@"; do
  echo "== $bot"
  out=$(go run ./cmd/tools/play_as "$bot" < "$file" 2>&1 | grep -E 'Installed|Uninstalled|Transferred|Error|cpu')
  printf '%s\n' "$out"
  rm -f "data/agents/$bot/play_as.lease"
  if printf '%s\n' "$out" | grep -q 'Error'; then
    failed+=("$bot")
  fi
done
if [ ${#failed[@]} -gt 0 ]; then
  echo "ERRORS on: ${failed[*]}" >&2
  exit 1
fi
