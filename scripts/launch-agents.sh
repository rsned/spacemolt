#!/bin/bash

# Comprehensive Agent Launcher - Manages all 90 autonomous mining agents

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"
START_DIR="$SCRIPT_DIR/../"

# Define all agent types and their counts
declare -A AGENT_TYPES
AGENT_TYPES=(
    ["assist"]=5
    ["craftsman"]=10
    ["engineer"]=10
    ["explorer"]=10
    ["fighter"]=10
    ["miner"]=10
    ["pirate"]=15
    ["prophet"]=2
    ["random"]=10
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

# Extract role from agent name (e.g., "pirate-1" -> "pirate")
get_agent_role() {
    echo "$1" | sed 's/-[0-9]*$//'
}

# Roles that have been migrated to skill-runner
SKILL_RUNNER_ROLES="miner assist salvager pirate"

# Get binary name for a role
get_binary_for_role() {
    local role=$1
    if echo "$SKILL_RUNNER_ROLES" | grep -qw "$role"; then
        echo "skill-runner"
    else
        echo "auto-${role}"
    fi
}

# Get launch args for an agent
get_agent_args() {
    local agent=$1
    local role=$(get_agent_role "$agent")
    if echo "$SKILL_RUNNER_ROLES" | grep -qw "$role"; then
        echo "--agent $agent"
    else
        echo "$agent"
    fi
}

case "$1" in
  start)
    FORCE_BINARY=""
    TYPE_FILTER=""

    # Parse arguments
    shift
    while [[ $# -gt 0 ]]; do
        case $1 in
            --binary)
                FORCE_BINARY="$2"
                shift 2
                ;;
            *)
                TYPE_FILTER="$1"
                shift
                ;;
        esac
    done

    # Validate forced binary if specified
    if [ -n "$FORCE_BINARY" ]; then
        FORCE_BINARY="auto-${FORCE_BINARY#auto-}"  # Normalize: add "auto-" prefix if missing
        if [ ! -f "../bin/$FORCE_BINARY" ]; then
            echo "❌ Binary not found: ../bin/$FORCE_BINARY"
            exit 1
        fi
        echo "🔧 Override mode: Using $FORCE_BINARY for all agents"
    fi

    # Determine which agents to start
    if [ -z "$TYPE_FILTER" ]; then
        echo "🚀 Starting ALL $TOTAL_AGENTS agents..."
        AGENTS=$(get_all_agents)
    else
        if [ -z "${AGENT_TYPES[$TYPE_FILTER]}" ]; then
            echo "❌ Unknown agent type: $TYPE_FILTER"
            echo "Valid types: ${!AGENT_TYPES[@]}"
            exit 1
        fi
        echo "🚀 Starting all $TYPE_FILTER agents..."
        AGENTS=$(get_agents_by_type "$TYPE_FILTER")
    fi

    mkdir -p logs

    STARTED=0
    for agent in $AGENTS; do
        # Determine which binary to use
        if [ -n "$FORCE_BINARY" ]; then
            binary="$FORCE_BINARY"
        else
            role=$(get_agent_role "$agent")
            binary=$(get_binary_for_role "$role")
        fi

        # Check if already running (check for auto-* or skill-runner with this agent)
        if pgrep -f "(auto-|skill-runner).* $agent" > /dev/null; then
            echo "  ⚠️  $agent already running, skipping"
            continue
        fi

        args=$(get_agent_args "$agent")
        (cd "$START_DIR" && ./bin/$binary --transport=mcp --debug=1 $args > logs/$agent.log 2>&1 &)
        echo "  ✓ Started $agent with $binary (PID: $!)"
	sleep 3
        STARTED=$((STARTED + 1))

        # Small delay to avoid overwhelming the connection
        if [ $((STARTED % 10)) -eq 0 ]; then
            sleep 1
        fi
    done

    sleep 2
    RUNNING=$(pgrep -f "bin/(auto-|skill-runner)" | wc -l)
    echo "✅ Started $STARTED agents, $RUNNING total running"
    ;;

  stop)
    echo "🛑 Stopping all agents..."
    pkill -f "bin/auto-" 2>/dev/null
    pkill -f "bin/skill-runner" 2>/dev/null
    sleep 2

    # Force kill if any still running
    if pgrep -f "bin/(auto-|skill-runner)" > /dev/null; then
        echo "⚠️  Some agents still running, force killing..."
        pkill -9 -f "bin/auto-" 2>/dev/null
        pkill -9 -f "bin/skill-runner" 2>/dev/null
    fi

    echo "✅ All agents stopped"
    ;;

  restart)
    echo "🔄 Restarting agents..."
    $0 stop
    sleep 3
    shift
    $0 start "$@"
    ;;

  status)
    RUNNING=$(pgrep -f "bin/(auto-|skill-runner)" | wc -l)
    echo "📊 Agent Status"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Total agents configured: $TOTAL_AGENTS"
    echo "Currently running: $RUNNING"
    echo ""

    if [ $RUNNING -eq 0 ]; then
        echo "❌ No agents running"
    else
        echo "Running agents:"
        pgrep -f "bin/(auto-|skill-runner)" | while read pid; do
            cmdline=$(ps -p $pid -o args= 2>/dev/null)
            agent=$(echo "$cmdline" | grep -oP '(auto-[a-z]+ |--agent )\K[^ ]+' || echo "unknown")
            echo "  ✓ $agent (PID: $pid)"
        done
    fi

    echo ""
    echo "Status by type:"
    for type in "${!AGENT_TYPES[@]}"; do
        count=${AGENT_TYPES[$type]}
        running=0
        binary=$(get_binary_for_role "$type")
        for i in $(seq 1 $count); do
            agent="${type}-${i}"
            if pgrep -f "(auto-|skill-runner).* $agent" > /dev/null; then
                running=$((running + 1))
            fi
        done
        printf "  %-12s: %2d / %d running (%s)\n" "$type" $running $count "$binary"
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
        binary=$(get_binary_for_role "$type")
        echo "═══ $type AGENTS ═══"
        running=0
        total_credits=0
        credits_found=0

        for i in $(seq 1 $count); do
            agent="${type}-${i}"

            # Check if running
            if pgrep -f "$binary $agent" > /dev/null; then
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
    total_running=$(pgrep -f "bin/(auto-|skill-runner)" | wc -l)
    echo "Running: $total_running / $TOTAL_AGENTS agents"
    echo ""
    ;;

  credits)
    echo "💰 CREDIT LEADERBOARD (ALL AGENTS)"
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
        sort -rn "$tmpfile" | while read line; do
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
    if [ -z "$2" ]; then
        echo "🔨 Rebuilding all agent binaries..."
        failed=0

        # Build skill-runner first (used by migrated agents)
        echo "  Building skill-runner..."
        (cd "$START_DIR" && go build -o bin/skill-runner cmd/skill-runner/main.go)
        if [ $? -ne 0 ]; then
            echo "  ❌ Build failed for skill-runner"
            failed=$((failed + 1))
        fi

        # Build remaining auto-* binaries (for non-migrated agents)
        built_binaries=""
        for type in "${!AGENT_TYPES[@]}"; do
            binary=$(get_binary_for_role "$type")
            # Skip skill-runner (already built) and dedup
            if [ "$binary" = "skill-runner" ] || echo "$built_binaries" | grep -qw "$binary"; then
                continue
            fi
            built_binaries="$built_binaries $binary"
            if [ -d "$START_DIR/cmd/$binary" ]; then
                echo "  Building $binary..."
                (cd "$START_DIR" && go build -o "bin/$binary" "cmd/$binary/main.go")
                if [ $? -ne 0 ]; then
                    echo "  ❌ Build failed for $binary"
                    failed=$((failed + 1))
                fi
            else
                echo "  ⚠️  No cmd/$binary directory found, skipping"
            fi
        done

        if [ $failed -eq 0 ]; then
            echo "✅ All builds successful"
            echo ""
            read -p "Restart agents with new versions? (y/n) " -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                $0 restart
            fi
        else
            echo "❌ $failed build(s) failed"
            exit 1
        fi
    else
        binary=$(get_binary_for_role "$2")
        echo "🔨 Rebuilding $binary..."
        if [ ! -d "cmd/$binary" ]; then
            echo "❌ No cmd/$binary directory found"
            exit 1
        fi
        go build -o "bin/$binary" "cmd/$binary/main.go"
        if [ $? -eq 0 ]; then
            echo "✅ Build successful"
            echo ""
            read -p "Restart $2 agents with new version? (y/n) " -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                $0 restart "$2"
            fi
        else
            echo "❌ Build failed"
            exit 1
        fi
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
    echo "  start [type] [--binary <name>]  - Start all agents or specific type"
    echo "  stop                            - Stop all agents"
    echo "  restart [type] [--binary <name>]- Restart all agents or specific type"
    echo "  status                          - Show running status of all agents"
    echo "  watch [agent]                   - Watch logs (all or specific agent)"
    echo "  tail <agent>                    - Watch specific agent log"
    echo "  summary                         - Show fleet summary"
    echo "  credits                         - Show credit leaderboard"
    echo "  upgrades                        - Show recent upgrades"
    echo "  errors                          - Show recent errors"
    echo "  rebuild [type]                  - Rebuild all binaries or specific type"
    echo "  types                           - List available agent types"
    echo ""
    echo "Examples:"
    echo "  $0 start                           # Start all agents (role-based binaries)"
    echo "  $0 start --binary explorer         # Start ALL agents with auto-explorer"
    echo "  $0 restart --binary trader         # Restart all with auto-trader"
    echo "  $0 status                          # Check fleet status"
    echo "  $0 watch                           # Watch all logs"
    echo "  $0 tail miner-1                    # Watch specific agent"
    echo "  $0 credits                         # See credit leaderboard"
    echo ""
    echo "Agent Types: ${!AGENT_TYPES[@]}"
    exit 1
    ;;
esac
