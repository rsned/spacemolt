#!/bin/bash

# Staggered Agent Launcher - Starts agents in batches to avoid rate limiting
#
# Usage:
#   ./start-agents-staggered.sh              # Start all agents with role-based binaries
#   ./start-agents-staggered.sh --binary miner   # Start all agents with auto-miner
#   ./start-agents-staggered.sh --strategy craft-sell  # Use craft-sell strategy for miners

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

mkdir -p logs

# Show help
show_help() {
    cat << EOF
Staggered Agent Launcher - Starts agents in batches to avoid rate limiting

Usage:
  $0 [options]

Options:
  --binary <name>     Use specific binary for all agents (e.g., miner, trader, fighter)
  --strategy <str>    Mining strategy for agents running auto-miner (default: sell)
                      Valid strategies: sell, craft-sell, craft-deposit

Examples:
  $0                                    # Start all agents with role-based binaries
  $0 --binary miner                     # Start all agents as miners
  $0 --strategy craft-sell              # Use craft-sell for all miner agents
  $0 --binary miner --strategy sell     # Start all as miners with sell strategy

Mining Strategies:
  sell              Sell all cargo immediately (default, fastest)
  craft-sell        Craft items from resources, then sell all
  craft-deposit     Craft items from resources, then deposit to storage

Note:
  The --strategy flag applies to ANY agent running auto-miner, whether they are
  originally miner-* agents or started with --binary miner.

EOF
}

# Handle help flag
if [ "$1" == "-h" ] || [ "$1" == "--help" ]; then
    show_help
    exit 0
fi
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
MINER_STRATEGY=""
STRATEGY_ARG=""

while [ $# -gt 0 ]; do
    case "$1" in
        --binary)
            FORCE_BINARY="auto-${2#auto-}"  # Normalize: add "auto-" prefix if missing
            if [ ! -f "../bin/$FORCE_BINARY" ]; then
                echo "❌ Binary not found: bin/$FORCE_BINARY"
                exit 1
            fi
            echo "🔧 Override mode: Using $FORCE_BINARY for all agents"
            echo ""
            shift 2
            ;;
        --strategy)
            MINER_STRATEGY="$2"
            # Validate strategy
            case "$MINER_STRATEGY" in
                sell|craft-sell|craft-deposit)
                    STRATEGY_ARG="$MINER_STRATEGY"
                    ;;
                *)
                    echo "❌ Invalid strategy: $MINER_STRATEGY"
                    echo "   Valid strategies: sell, craft-sell, craft-deposit"
                    exit 1
                    ;;
            esac
            echo "⚙️  Miner strategy: $MINER_STRATEGY"
            echo ""
            shift 2
            ;;
        *)
            echo "❌ Unknown option: $1"
            echo "Usage: $0 [--binary binary] [--strategy strategy]"
            exit 1
            ;;
    esac
done

echo "🚀 Starting agents in batches to avoid rate limiting..."
echo ""

# Show current configuration
if [ -n "$FORCE_BINARY" ]; then
    echo "📋 Configuration:"
    echo "   Binary: $FORCE_BINARY (all agents)"
    echo ""
fi
if [ -n "$MINER_STRATEGY" ]; then
    echo "📋 Configuration:"
    echo "   Miner Strategy: $MINER_STRATEGY"
    echo ""
fi

# Define agent batches
BATCHES=(
    "assist-haven assist-krynn assist-nexus assist-frontier assist-sol"
    "prophet-1 prophet-2"
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
    "random-6 random-7 random-8 random-9 random-npc"
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
        # Add strategy argument if strategy is specified and using auto-miner binary
        AGENT_ARGS="$agent"
        SHOW_STRATEGY=""
        if [ -n "$STRATEGY_ARG" ] && [[ "$binary" == "auto-miner" ]]; then
            AGENT_ARGS="$agent $STRATEGY_ARG"
            SHOW_STRATEGY=" (strategy: $STRATEGY_ARG)"
        fi

        (cd "$SCRIPT_DIR/.." && ./bin/$binary --transport=mcp $AGENT_ARGS > logs/$agent.log 2>&1 &)
        echo "  ✓ Started $agent with $binary$SHOW_STRATEGY"
        TOTAL_STARTED=$((TOTAL_STARTED + 1))

        # Delay between starts to avoid IP rate limiting on login
        sleep 5
    done

    # Wait between batches to avoid rate limiting
    REMAINING=$((90 - TOTAL_STARTED))
    echo "  (Started $TOTAL_STARTED/90, $REMAINING remaining)"
    echo ""

    if [ $BATCH_NUM -lt ${#BATCHES[@]} ]; then
        echo "⏸️  Waiting 20 seconds before next batch..."
        sleep 20
        echo ""
    fi
done

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Launch complete! Started $TOTAL_STARTED agents"
echo ""
echo "Waiting 11 seconds for connections to establish..."
sleep 11

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
