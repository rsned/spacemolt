#!/bin/bash

# Dynamically detect and start missing agents

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

AGENTS_DIR="$SCRIPT_DIR/data/agents"

# Count expected agents
EXPECTED_AGENTS=$(find "$AGENTS_DIR" -mindepth 1 -maxdepth 1 -type d | wc -l)

echo "🔍 Detecting missing agents..."
echo "   Expected: $EXPECTED_AGENTS agents defined in data/agents/"
echo ""

# Get list of all agent directories (exclude files like README.md, agent_names.txt, etc.)
ALL_AGENTS=$(find "$AGENTS_DIR" -mindepth 1 -maxdepth 1 -type d -exec basename {} \; | sort)

# Find agents that are NOT running
MISSING_AGENTS=()
for agent in $ALL_AGENTS; do
    if ! pgrep -f "auto-miner $agent" > /dev/null; then
        MISSING_AGENTS+=("$agent")
    fi
done

MISSING_COUNT=${#MISSING_AGENTS[@]}

# Check if any agents are missing
if [ $MISSING_COUNT -eq 0 ]; then
    echo "✅ All agents are already running!"
    exit 0
fi

echo "🔄 Starting $MISSING_COUNT missing agents..."
echo ""

STARTED=0
for agent in "${MISSING_AGENTS[@]}"; do
    # Double-check if already running (might have started since we checked)
    if pgrep -f "auto-miner $agent" > /dev/null; then
        echo "  ✓ $agent already running"
        continue
    fi

    # Check if agent directory exists
    if [ ! -d "$AGENTS_DIR/$agent" ]; then
        echo "  ⚠️  $agent directory not found, skipping"
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
echo "📊 Total agents now running: $RUNNING/$EXPECTED_AGENTS"

if [ $RUNNING -eq $EXPECTED_AGENTS ]; then
    echo "✅ SUCCESS! All $EXPECTED_AGENTS agents are now running!"
else
    STILL_MISSING=$((EXPECTED_AGENTS - RUNNING))
    echo "⚠️  Still missing $STILL_MISSING agents"
fi
