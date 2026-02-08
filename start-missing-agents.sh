#!/bin/bash

# Start the 15 agents that failed to connect

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

echo "🔄 Attempting to start the 15 missing agents..."
echo ""

# Define missing agents
MISSING_AGENTS=(
    "miner-6"
    "miner-7"
    "miner-8"
    "miner-9"
    "miner-10"
    "explorer-1"
    "explorer-2"
    "explorer-3"
    "explorer-4"
    "trader-6"
    "trader-7"
    "trader-8"
    "trader-9"
    "trader-10"
)

STARTED=0
for agent in "${MISSING_AGENTS[@]}"; do
    # Check if already running
    if pgrep -f "auto-miner $agent" > /dev/null; then
        echo "  ✓ $agent already running"
        continue
    fi

    # Start the agent
    (cd "$SCRIPT_DIR" && ./bin/auto-miner $agent > logs/$agent.log 2>&1 &)
    echo "  → Starting $agent..."
    STARTED=$((STARTED + 1))

    # 3 second delay between starts
    sleep 3
done

echo ""
echo "Waiting 5 seconds for connections to establish..."
sleep 5

# Check results
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
RUNNING=$(pgrep -f "bin/auto-miner" | wc -l)
echo "📊 Total agents now running: $RUNNING/90"

if [ $RUNNING -eq 90 ]; then
    echo "✅ SUCCESS! All 90 agents are now running!"
else
    MISSING=$((90 - RUNNING))
    echo "⚠️  Still missing $MISSING agents"
fi
