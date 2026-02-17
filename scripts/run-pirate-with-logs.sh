#!/bin/bash

# Run a pirate with logging
# Usage: ./run-pirate-with-logs.sh <pirate-number> [duration-in-seconds]

if [ $# -lt 1 ]; then
    echo "Usage: $0 <pirate-number> [duration]"
    echo "Example: $0 1        # Run pirate-1 indefinitely"
    echo "Example: $0 2 300    # Run pirate-2 for 5 minutes"
    exit 1
fi

PIRATE_NUM=$1
DURATION=${2:-0}  # Default to 0 (run indefinitely)
LOG_DIR="logs"
LOG_FILE="$LOG_DIR/pirate-$PIRATE_NUM.log"

# Create logs directory if it doesn't exist
mkdir -p "$LOG_DIR"

echo "Starting pirate-$PIRATE_NUM..."
echo "Logs will be saved to: $LOG_FILE"
echo "To follow logs: tail -f $LOG_FILE"
echo ""

if [ "$DURATION" -gt 0 ]; then
    echo "Running for $DURATION seconds..."
    timeout "$DURATION" ./bin/play-pirates "$PIRATE_NUM" 2>&1 | tee "$LOG_FILE"
else
    echo "Running indefinitely (Ctrl+C to stop)..."
    ./bin/play-pirates "$PIRATE_NUM" 2>&1 | tee "$LOG_FILE"
fi
