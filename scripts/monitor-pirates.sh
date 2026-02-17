#!/bin/bash

# Monitor all pirate progress
clear
echo "🏴‍☠️ PIRATE CREW MONITORING DASHBOARD 🏴‍☠️"
echo "========================================"
echo ""

while true; do
    # Move cursor to top
    tput cup 3 0

    for i in {1..5}; do
        LOG_FILE="logs/pirate-$i.log"

        if [ -f "$LOG_FILE" ]; then
            # Get pirate name
            NAME=$(grep "🏴‍☠️" "$LOG_FILE" | head -1 | awk -F'🏴‍☠️ ' '{print $2}' | awk -F' (' '{print $1}')

            # Get latest status
            LATEST=$(grep "\[TICK" "$LOG_FILE" | tail -1)

            # Get login status
            LOGIN=$(grep "Logged in" "$LOG_FILE" | tail -1)

            # Get actions
            UNDOCK=$(grep -c "Undocking" "$LOG_FILE")
            MINING=$(grep -c "Points of Interest" "$LOG_FILE")

            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "Pirate $i: $NAME"
            if [ -n "$LOGIN" ]; then
                echo "  Status: $(echo $LOGIN | grep -o 'Credits: [0-9.]*' | head -1)"
                echo "  System: $(echo $LOGIN | grep -o 'System: [^,]*' | head -1)"
            fi
            if [ -n "$LATEST" ]; then
                echo "  Latest: $LATEST"
            fi
            echo "  Actions: Undocked=$UNDOCK times"
            echo ""
        else
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "Pirate $i: Waiting for log..."
            echo ""
        fi
    done

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Last update: $(date '+%H:%M:%S')"
    echo "Press Ctrl+C to exit"

    sleep 3
done
