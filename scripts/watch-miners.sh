#!/bin/bash

# Real-time mining dashboard
echo "🏴‍☠️ PIRATE CREW AUTONOMOUS MINING DASHBOARD 🏴‍☠️"
echo "================================================"
echo ""

watch -n 3 '
echo "Last Updated: $(date +%T)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
for i in {1..5}; do
    LOG="logs/pirate-$i-mining.log"
    if [ -f "$LOG" ]; then
        echo ""
        echo "PIRATE-$i:"

        # Get latest mining run
        RUN=$(grep "Mining Run" "$LOG" | tail -1)
        if [ -n "$RUN" ]; then
            echo "  $RUN"
        fi

        # Get latest status
        CREDITS=$(grep "Credits:" "$LOG" | tail -1 | grep -o "Credits: [0-9.]*" | head -1)
        FUEL=$(grep "Fuel:" "$LOG" | tail -1 | grep -o "Fuel: [0-9./]*" | head -1)

        if [ -n "$CREDITS" ]; then
            echo "  $CREDITS | $FUEL"
        fi

        # Get latest action
        ACTION=$(grep -E "(Mining\.\.\.|Traveling|Undocking|Docking|Selling)" "$LOG" | tail -1)
        if [ -n "$ACTION" ]; then
            TIMESTAMP=$(echo "$ACTION" | awk "{print \$1, \$2}")
            MESSAGE=$(echo "$ACTION" | cut -d"]" -f2-)
            echo "  Last: $MESSAGE"
        fi

        # Count total mining operations
        MINE_COUNT=$(grep -c "Mining\.\.\." "$LOG")
        echo "  Total Mines: $MINE_COUNT"

    else
        echo ""
        echo "PIRATE-$i: Log not found"
    fi
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
done
'
