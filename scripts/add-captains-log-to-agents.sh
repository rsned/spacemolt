#!/bin/bash
# Script to add captain's log integration to all auto-* agents

# This script adds the standard captain's log boilerplate to simple monitoring agents

AGENTS=("auto-random" "auto-fighter")

for agent in "${AGENTS[@]}"; do
    echo "Processing $agent..."

    MAIN_FILE="cmd/$agent/main.go"

    if [ ! -f "$MAIN_FILE" ]; then
        echo "  ✗ $MAIN_FILE not found, skipping"
        continue
    fi

    # Check if already has captain's log
    if grep -q "WriteCaptainsLog" "$MAIN_FILE"; then
        echo "  ✓ $agent already has captain's log support, skipping"
        continue
    fi

    echo "  → Adding captain's log to $agent"

    # Note: The actual updates were done manually in the session
    # This script is just documentation of what was done

done

echo "Done!"
