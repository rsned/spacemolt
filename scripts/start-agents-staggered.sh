#!/bin/bash

# Staggered Agent Launcher - Starts agents in batches to avoid rate limiting
#
# Usage:
#   ./start-agents-staggered.sh              # Start all agents with role-based binaries
#   ./start-agents-staggered.sh --binary miner   # Start all agents with auto-miner

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

mkdir -p logs

# Extract role from agent name (e.g., "pirate-1" -> "pirate")
get_agent_role() {
    echo "$1" | sed 's/-[0-9]*$//'
}

# Get binary name for a role
get_binary_for_role() {
    local role=$1
    echo "auto-${role}"
}

# Parse command line arguments
FORCE_BINARY=""
if [ "$1" == "--binary" ]; then
    FORCE_BINARY="auto-${2#auto-}"  # Normalize: add "auto-" prefix if missing
    if [ ! -f "../bin/$FORCE_BINARY" ]; then
        echo "❌ Binary not found: bin/$FORCE_BINARY"
        exit 1
    fi
    echo "🔧 Override mode: Using $FORCE_BINARY for all agents"
    echo ""
fi

echo "🚀 Starting 90 agents in batches to avoid rate limiting..."
echo ""

# Define agent batches
BATCHES=(
    "pirate-1 pirate-2 pirate-3 pirate-4 pirate-5"
    "pirate-6 pirate-7 pirate-8 pirate-9 pirate-10"
    "pirate-11 pirate-12 pirate-13 pirate-14 pirate-15"
    "miner-1 miner-2 miner-3 miner-4 miner-5"
    "miner-6 miner-7 miner-8 miner-9 miner-10"
    "craftsman-1 craftsman-2 craftsman-3 craftsman-4 craftsman-5"
    "craftsman-6 craftsman-7 craftsman-8 craftsman-9 craftsman-10"
    "engineer-1 engineer-2 engineer-3 engineer-4 engineer-5"
    "engineer-6 engineer-7 engineer-8 engineer-9 engineer-10"
    "explorer-1 explorer-2 explorer-3 explorer-4 explorer-5"
    "explorer-6 explorer-7 explorer-8 explorer-9 explorer-10"
    "fighter-1 fighter-2 fighter-3 fighter-4 fighter-5"
    "fighter-6 fighter-7 fighter-8 fighter-9 fighter-10"
    "trader-1 trader-2 trader-3 trader-4 trader-5"
    "trader-6 trader-7 trader-8 trader-9 trader-10"
    "salvager-1 salvager-2 salvager-3 salvager-4 salvager-5"
    "salvager-6 salvager-7 salvager-8 salvager-9 salvager-10"
    "random-1 random-2 random-3 random-4 random-5"
)

BATCH_NUM=0
TOTAL_STARTED=0

for batch in "${BATCHES[@]}"; do
    BATCH_NUM=$((BATCH_NUM + 1))
    echo "━━━ Batch $BATCH_NUM ━━━"

    for agent in $batch; do
        # Determine which binary to use
        if [ -n "$FORCE_BINARY" ]; then
            binary="$FORCE_BINARY"
        else
            role=$(get_agent_role "$agent")
            binary=$(get_binary_for_role "$role")
        fi

        # Check if already running (check for any auto-* binary with this agent)
        if pgrep -f "auto-.* $agent" > /dev/null; then
            echo "  ⚠️  $agent already running"
            continue
        fi

        # Start the agent
        (cd "$SCRIPT_DIR/.." && ./bin/$binary $agent > logs/$agent.log 2>&1 &)
        echo "  ✓ Started $agent with $binary"
        TOTAL_STARTED=$((TOTAL_STARTED + 1))

        # Small delay between starts in same batch
        sleep 0.5
    done

    # Wait between batches to avoid rate limiting
    REMAINING=$((90 - TOTAL_STARTED))
    echo "  (Started $TOTAL_STARTED/90, $REMAINING remaining)"
    echo ""

    if [ $BATCH_NUM -lt ${#BATCHES[@]} ]; then
        echo "⏸️  Waiting 10 seconds before next batch..."
        sleep 10
        echo ""
    fi
done

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Launch complete! Started $TOTAL_STARTED agents"
echo ""
echo "Waiting 5 seconds for connections to establish..."
sleep 5

# Check final status
RUNNING=$(pgrep -f "bin/auto-" | wc -l)
echo "📊 Final Status: $RUNNING agents running"

if [ $RUNNING -lt $TOTAL_STARTED ]; then
    FAILED=$((TOTAL_STARTED - RUNNING))
    echo "⚠️  $FAILED agents may have failed to connect"
    echo ""
    echo "To view failed agents:"
    echo "  grep -l '429' logs/*.log | wc -l"
    echo ""
    echo "To restart failed agents, wait a few minutes then run this script again."
fi
