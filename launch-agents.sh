#!/bin/bash

# Comprehensive Agent Launcher - Manages all 90 autonomous mining agents

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# Define all agent types and their counts
declare -A AGENT_TYPES
AGENT_TYPES=(
    ["craftsman"]=10
    ["engineer"]=10
    ["explorer"]=10
    ["fighter"]=10
    ["miner"]=10
    ["pirate"]=15
    ["random"]=5
    ["salvager"]=10
    ["trader"]=10
)

# Calculate total agents
TOTAL_AGENTS=0
for count in "${AGENT_TYPES[@]}"; do
    TOTAL_AGENTS=$((TOTAL_AGENTS + count))
done

# Get list of all agent IDs
get_all_agents() {
    for type in "${!AGENT_TYPES[@]}"; do
        count=${AGENT_TYPES[$type]}
        for i in $(seq 1 $count); do
            echo "${type}-${i}"
        done
    done
}

# Get agents of a specific type
get_agents_by_type() {
    local type=$1
    local count=${AGENT_TYPES[$type]}
    for i in $(seq 1 $count); do
        echo "${type}-${i}"
    done
}

case "$1" in
  start)
    if [ -z "$2" ]; then
        echo "🚀 Starting ALL $TOTAL_AGENTS agents..."
        AGENTS=$(get_all_agents)
    else
        if [ -z "${AGENT_TYPES[$2]}" ]; then
            echo "❌ Unknown agent type: $2"
            echo "Valid types: ${!AGENT_TYPES[@]}"
            exit 1
        fi
        echo "🚀 Starting all $2 agents..."
        AGENTS=$(get_agents_by_type "$2")
    fi

    mkdir -p logs

    STARTED=0
    for agent in $AGENTS; do
        # Check if already running
        if pgrep -f "auto-miner $agent" > /dev/null; then
            echo "  ⚠️  $agent already running, skipping"
            continue
        fi

        (cd "$SCRIPT_DIR" && ./bin/auto-miner $agent > logs/$agent.log 2>&1 &)
        echo "  ✓ Started $agent (PID: $!)"
        STARTED=$((STARTED + 1))

        # Small delay to avoid overwhelming the connection
        if [ $((STARTED % 10)) -eq 0 ]; then
            sleep 1
        fi
    done

    sleep 2
    RUNNING=$(pgrep -f "bin/auto-miner" | wc -l)
    echo "✅ Started $STARTED agents, $RUNNING total running"
    ;;

  stop)
    echo "🛑 Stopping all agents..."
    pkill -f "bin/auto-miner"
    sleep 2

    # Force kill if any still running
    if pgrep -f "bin/auto-miner" > /dev/null; then
        echo "⚠️  Some agents still running, force killing..."
        pkill -9 -f "bin/auto-miner"
    fi

    echo "✅ All agents stopped"
    ;;

  restart)
    echo "🔄 Restarting agents..."
    $0 stop
    sleep 3
    $0 start "${@:2}"
    ;;

  status)
    RUNNING=$(pgrep -f "bin/auto-miner" | wc -l)
    echo "📊 Agent Status"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Total agents configured: $TOTAL_AGENTS"
    echo "Currently running: $RUNNING"
    echo ""

    if [ $RUNNING -eq 0 ]; then
        echo "❌ No agents running"
    else
        echo "Running agents:"
        pgrep -f "bin/auto-miner" | while read pid; do
            cmdline=$(ps -p $pid -o args= 2>/dev/null)
            agent=$(echo "$cmdline" | grep -oP 'auto-miner \K[^ ]+' || echo "unknown")
            echo "  ✓ $agent (PID: $pid)"
        done
    fi

    echo ""
    echo "Status by type:"
    for type in "${!AGENT_TYPES[@]}"; do
        count=${AGENT_TYPES[$type]}
        running=0
        for i in $(seq 1 $count); do
            if pgrep -f "auto-miner $type-$i" > /dev/null; then
                running=$((running + 1))
            fi
        done
        printf "  %-12s: %2d / %d running\n" "$type" $running $count
    done
    ;;

  watch)
    if [ -z "$2" ]; then
        echo "👀 Watching ALL agent logs (Ctrl+C to exit)..."
        tail -f logs/*.log 2>/dev/null
    else
        if [ ! -f "logs/$2.log" ]; then
            echo "❌ Log file not found: logs/$2.log"
            exit 1
        fi
        echo "👀 Watching $2 (Ctrl+C to exit)..."
        tail -f logs/$2.log
    fi
    ;;

  summary)
    echo "📊 AGENT FLEET SUMMARY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Generated: $(date)"
    echo ""

    for type in "${!AGENT_TYPES[@]}"; do
        count=${AGENT_TYPES[$type]}
        echo "═══ $type AGENTS ═══"
        running=0
        total_credits=0
        credits_found=0

        for i in $(seq 1 $count); do
            agent="${type}-${i}"

            # Check if running
            if pgrep -f "auto-miner $agent" > /dev/null; then
                running=$((running + 1))
            fi

            # Get latest credits
            if [ -f "logs/$agent.log" ]; then
                CREDITS=$(tail -100 "logs/$agent.log" 2>/dev/null | grep "Current Credits:" | tail -1 | grep -oP 'Current Credits: \K[0-9.]+')
                if [ -n "$CREDITS" ]; then
                    total_credits=$(echo "$total_credits + $CREDITS" | bc)
                    credits_found=$((credits_found + 1))
                fi
            fi
        done

        printf "Status: %d / %d running\n" $running $count

        if [ $credits_found -gt 0 ]; then
            avg_credits=$(echo "scale=0; $total_credits / $credits_found" | bc)
            printf "Credits: %.0f total (avg: %.0f per active agent)\n" $total_credits $avg_credits
        else
            echo "Credits: No data yet"
        fi

        echo ""
    done

    echo "═══ FLEET TOTALS ═══"
    total_running=$(pgrep -f "bin/auto-miner" | wc -l)
    echo "Running: $total_running / $TOTAL_AGENTS agents"
    echo ""
    ;;

  credits)
    echo "💰 CREDIT LEADERBOARD"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Create temp file for sorting
    tmpfile=$(mktemp)
    for type in "${!AGENT_TYPES[@]}"; do
        count=${AGENT_TYPES[$type]}
        for i in $(seq 1 $count); do
            agent="${type}-${i}"
            if [ -f "logs/$agent.log" ]; then
                CREDITS=$(tail -100 "logs/$agent.log" 2>/dev/null | grep "Current Credits:" | tail -1 | grep -oP 'Current Credits: \K[0-9.]+')
                if [ -n "$CREDITS" ]; then
                    echo "$CREDITS $agent" >> "$tmpfile"
                fi
            fi
        done
    done

    if [ -s "$tmpfile" ]; then
        sort -rn "$tmpfile" | head -20 | while read line; do
            credits=$(echo "$line" | awk '{print $1}')
            agent=$(echo "$line" | awk '{print $2}')
            printf "  %-20s %12.0f credits\n" "$agent" $credits
        done
    else
        echo "No credit data available yet"
    fi

    rm -f "$tmpfile"
    ;;

  upgrades)
    echo "🔧 RECENT UPGRADES"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    grep -h "⛏️  Buying\|📦 Buying\|🛡️  Buying\|🔫 Buying\|✅.*INSTALLED" logs/*.log 2>/dev/null | tail -30
    if [ ${PIPESTATUS[0]} -ne 0 ]; then
        echo "No upgrades found yet"
    fi
    ;;

  errors)
    echo "❌ RECENT ERRORS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    grep -h "✗\|❌\|Error\|Failed" logs/*.log 2>/dev/null | tail -20
    if [ ${PIPESTATUS[0]} -ne 0 ]; then
        echo "No errors found (running clean!)"
    fi
    ;;

  tail)
    if [ -z "$2" ]; then
        echo "Usage: $0 tail <agent-id>"
        echo "Example: $0 tail miner-1"
        echo "Example: $0 tail pirate-3"
        exit 1
    fi
    if [ ! -f "logs/$2.log" ]; then
        echo "❌ Log file not found: logs/$2.log"
        echo ""
        echo "Available log files:"
        ls logs/*.log 2>/dev/null | xargs -n1 basename | head -10
        exit 1
    fi
    echo "📜 Watching $2 (Ctrl+C to exit)..."
    tail -f logs/$2.log
    ;;

  rebuild)
    echo "🔨 Rebuilding auto-miner..."
    go build -o bin/auto-miner cmd/auto-miner/main.go
    if [ $? -eq 0 ]; then
        echo "✅ Build successful"
        echo ""
        read -p "Restart agents with new version? (y/n) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            $0 restart "${@:2}"
        fi
    else
        echo "❌ Build failed"
        exit 1
    fi
    ;;

  types)
    echo "📋 Available Agent Types"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    for type in "${!AGENT_TYPES[@]}"; do
        count=${AGENT_TYPES[$type]}
        printf "  %-12s: %2d agents\n" "$type" $count
    done
    echo ""
    printf "  TOTAL:        %2d agents\n" $TOTAL_AGENTS
    ;;

  *)
    echo "🤖 Autonomous Agent Fleet Manager"
    echo ""
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  start [type]   - Start all agents or specific type"
    echo "  stop           - Stop all agents"
    echo "  restart [type] - Restart all agents or specific type"
    echo "  status         - Show running status of all agents"
    echo "  watch [agent]  - Watch logs (all or specific agent)"
    echo "  tail <agent>   - Watch specific agent log"
    echo "  summary        - Show fleet summary"
    echo "  credits        - Show credit leaderboard"
    echo "  upgrades       - Show recent upgrades"
    echo "  errors         - Show recent errors"
    echo "  rebuild        - Rebuild auto-miner binary"
    echo "  types          - List available agent types"
    echo ""
    echo "Examples:"
    echo "  $0 start                    # Start all 90 agents"
    echo "  $0 start pirate             # Start only pirate agents"
    echo "  $0 start miner              # Start only miner agents"
    echo "  $0 status                   # Check fleet status"
    echo "  $0 watch                    # Watch all logs"
    echo "  $0 tail miner-1             # Watch specific agent"
    echo "  $0 credits                  # See credit leaderboard"
    echo ""
    echo "Agent Types: ${!AGENT_TYPES[@]}"
    exit 1
    ;;
esac
