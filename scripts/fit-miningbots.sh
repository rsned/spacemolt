#!/usr/bin/env bash
# Usage: fit-miningbots.sh <ice|gas> <bot-id>...
# Pipes data/scripts/fit_<group>.txt into play_as for each bot.
set -uo pipefail
cd "$(dirname "$0")/.."
if [ $# -lt 2 ]; then
  echo "usage: $0 <ice|gas> <bot-id>..." >&2
  exit 2
fi
group=$1
shift
case "$group" in
  ice) want='Installed Ice Harvester II' ;;
  gas) want='Installed Gas Harvester III' ;;
  *) echo "group must be exactly 'ice' or 'gas'" >&2; exit 2 ;;
esac
file="data/scripts/fit_${group}.txt"
[ -f "$file" ] || { echo "missing $file" >&2; exit 2; }
for bot in "$@"; do
  if [ "$bot" = "miningbot_0000" ]; then
    echo "refusing miningbot_0000 (live worker)" >&2
    exit 2
  fi
done

cur=""
cleanup() { [ -n "$cur" ] && rm -f "data/agents/$cur/play_as.lease"; }
trap cleanup EXIT
trap 'exit 130' INT TERM

failed=()
for bot in "$@"; do
  echo "== $bot"
  cur=$bot
  raw=$(go run ./cmd/tools/play_as "$bot" < "$file" 2>&1)
  rc=$?
  out=$(printf '%s\n' "$raw" | grep -E 'Installed|Uninstalled|Transferred|Error|cpu')
  printf '%s\n' "$out"
  rm -f "data/agents/$bot/play_as.lease"
  cur=""
  if [ "$rc" -ne 0 ] || printf '%s\n' "$out" | grep -qi 'error' || ! printf '%s\n' "$out" | grep -q "$want"; then
    failed+=("$bot")
  fi
done
if [ ${#failed[@]} -gt 0 ]; then
  echo "ERRORS on: ${failed[*]}" >&2
  exit 1
fi
