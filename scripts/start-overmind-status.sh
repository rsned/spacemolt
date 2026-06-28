#!/bin/bash

# Start the overmind status page — a SINGLE standalone viewer that reads every
# overmind's *-status.json file and serves one combined HTML page (a table of
# contents across all overminds, then an alphabetical worker table per overmind).
#
# This is a singleton: it is NOT launched per-overmind. The overmind processes
# (haul/mb/shuttle) only write their status JSON; this one server reads all of
# them. Running it twice serves duplicate pages for no reason, so this script
# refuses to start a second copy.
#
# Usage:
#   ./scripts/start-overmind-status.sh                 # build (if needed) + start on :8087
#   PORT=9000 ./scripts/start-overmind-status.sh        # override listen port
#   REFRESH=120 ./scripts/start-overmind-status.sh      # override default auto-refresh (s)

set -euo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$( cd "$SCRIPT_DIR/.." && pwd )"
cd "$ROOT_DIR"

PORT="${PORT:-8087}"
REFRESH="${REFRESH:-300}"
BIN="bin/overmind-status"
LOG="data/overmind/overmind-status.log"

# Singleton guard: one viewer covers every overmind.
if pgrep -f "$BIN" > /dev/null; then
    echo "⚠️  overmind-status already running (one viewer covers all overminds). Not starting a second copy."
    echo "    pid(s): $(pgrep -f "$BIN" | tr '\n' ' ')"
    exit 0
fi

echo "🔨 Building $BIN ..."
go build -o "$BIN" ./cmd/tools/overmind-status

mkdir -p data/overmind
echo "🚀 Starting overmind-status on :$PORT (auto-refresh ${REFRESH}s) ..."
nohup "./$BIN" --addr ":$PORT" --refresh "$REFRESH" >> "$LOG" 2>&1 &
PID=$!

sleep 1
if kill -0 "$PID" 2>/dev/null; then
    echo "✅ overmind-status running (pid $PID)"
    echo "   View:  http://localhost:$PORT/"
    echo "   Log:   $LOG"
else
    echo "❌ overmind-status failed to start — see $LOG"
    tail -n 20 "$LOG" || true
    exit 1
fi
