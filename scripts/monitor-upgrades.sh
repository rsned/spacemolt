#!/bin/bash

# Monitor auto-miner agent upgrade progress
# Run this periodically to track how agents are progressing

echo "═══════════════════════════════════════════════════════════"
echo "AUTO-MINER UPGRADE PROGRESS MONITOR"
echo "Time: $(date)"
echo "═══════════════════════════════════════════════════════════"
echo ""

# Check if agent-status exists
if [ ! -f "./agent-status" ]; then
    echo "❌ Error: ./agent-status binary not found"
    echo "Please build it first: go build -o agent-status cmd/agent-status/main.go"
    exit 1
fi

# Get status data
echo "📊 FLEET OVERVIEW"
echo "─────────────────────────────────────────────────────────────"

# Count agents by ship class
echo ""
echo "🚀 Ship Distribution:"
./agent-status 2>/dev/null | grep "Ship:" | \
    awk '{print $2}' | \
    sort | uniq -c | sort -rn | \
    while read count ship; do
        printf "  %-20s %3d agents\n" "$ship" "$count"
    done

# Count agents by module count (proxy for mining lasers)
echo ""
echo "⛏️  Module Count Distribution (mining lasers installed):"
./agent-status 2>/dev/null | grep "Modules:" | \
    awk '{print $2}' | \
    sed 's/\[//' | \
    sort -n | uniq -c | sort -k2 -n | \
    while read count modules; do
        printf "  %-20s %3d agents\n" "$modules modules" "$count"
    done

# Credit distribution
echo ""
echo "💰 Credit Distribution:"
total_credits=$(./agent-status 2>/dev/null | grep "Credits:" | \
    awk '{sum+=$2} END {printf "%.0f", sum}')
agent_count=$(./agent-status 2>/dev/null | grep "Credits:" | wc -l)

if [ "$agent_count" -gt 0 ]; then
    avg_credits=$(echo "$total_credits $agent_count" | awk '{printf "%.0f", $1/$2}')
    echo "  Total Credits:       $(printf "%'d" "$total_credits")"
    echo "  Agent Count:         $agent_count"
    echo "  Average per Agent:   $(printf "%'d" "$avg_credits")"

    # Credit ranges
    echo ""
    echo "  Credit Ranges:"
    range_0_300=$(./agent-status 2>/dev/null | grep "Credits:" | \
        awk '{if ($2 < 300) count++} END {print count+0}')
    range_300_800=$(./agent-status 2>/dev/null | grep "Credits:" | \
        awk '{if ($2 >= 300 && $2 < 800) count++} END {print count+0}')
    range_800_2000=$(./agent-status 2>/dev/null | grep "Credits:" | \
        awk '{if ($2 >= 800 && $2 < 2000) count++} END {print count+0}')
    range_2000_5000=$(./agent-status 2>/dev/null | grep "Credits:" | \
        awk '{if ($2 >= 2000 && $2 < 5000) count++} END {print count+0}')
    range_5000_25000=$(./agent-status 2>/dev/null | grep "Credits:" | \
        awk '{if ($2 >= 5000 && $2 < 25000) count++} END {print count+0}')
    range_25000_plus=$(./agent-status 2>/dev/null | grep "Credits:" | \
        awk '{if ($2 >= 25000) count++} END {print count+0}')

    printf "    0 - 299 cr          %3d agents (waiting for tier 1)\n" "$range_0_300"
    printf "    300 - 799 cr        %3d agents (can buy tier 1)\n" "$range_300_800"
    printf "    800 - 1,999 cr      %3d agents (can buy tier 2)\n" "$range_800_2000"
    printf "    2,000 - 4,999 cr    %3d agents (can upgrade ship!)\n" "$range_2000_5000"
    printf "    5,000 - 24,999 cr   %3d agents (can buy mining_barge!)\n" "$range_5000_25000"
    printf "    25,000+ cr          %3d agents (can buy mining_cruiser!)\n" "$range_25000_plus"
fi

# Success metrics
echo ""
echo "✅ UPGRADE SUCCESS METRICS"
echo "─────────────────────────────────────────────────────────────"

total_agents=$(./agent-status 2>/dev/null | grep "Ship:" | wc -l)
starter_mining=$(./agent-status 2>/dev/null | grep "Ship: starter_mining" | wc -l)
mining_enhanced=$(./agent-status 2>/dev/null | grep "Ship: mining_enhanced" | wc -l)
mining_barge=$(./agent-status 2>/dev/null | grep "Ship: mining_barge" | wc -l)
mining_cruiser=$(./agent-status 2>/dev/null | grep "Ship: mining_cruiser" | wc -l)

if [ "$total_agents" -gt 0 ]; then
    pct_starter=$(echo "$starter_mining $total_agents" | \
        awk '{printf "%.1f", ($1/$2)*100}')
    pct_enhanced=$(echo "$mining_enhanced $total_agents" | \
        awk '{printf "%.1f", ($1/$2)*100}')
    pct_barge=$(echo "$mining_barge $total_agents" | \
        awk '{printf "%.1f", ($1/$2)*100}')
    pct_cruiser=$(echo "$mining_cruiser $total_agents" | \
        awk '{printf "%.1f", ($1/$2)*100}')

    printf "  starter_mining:     %3d agents (%5.1f%%)\n" \
        "$starter_mining" "$pct_starter"
    printf "  mining_enhanced:    %3d agents (%5.1f%%)\n" \
        "$mining_enhanced" "$pct_enhanced"
    printf "  mining_barge:       %3d agents (%5.1f%%)\n" \
        "$mining_barge" "$pct_barge"
    printf "  mining_cruiser:     %3d agents (%5.1f%%)\n" \
        "$mining_cruiser" "$pct_cruiser"
fi

# Check for stuck agents (starter_mining with lots of credits)
echo ""
echo "⚠️  POTENTIALLY STUCK AGENTS"
echo "─────────────────────────────────────────────────────────────"
stuck_count=$(./agent-status 2>/dev/null | \
    awk '/Ship: starter_mining/ {ship=1} /Credits:/ && ship {if ($2 >= 2000) print; ship=0}' | wc -l)

if [ "$stuck_count" -gt 0 ]; then
    echo "  Found $stuck_count agents with starter_mining + 2000+ credits"
    echo "  These should have upgraded to mining_enhanced!"
    echo ""
    echo "  Showing first 5:"
    ./agent-status 2>/dev/null | \
        awk '/Ship: starter_mining/ {ship=1; next} /Credits:/ && ship {if ($2 >= 2000) {print; if (++count >= 5) exit} ship=0}'
else
    echo "  ✓ No stuck agents detected - all progressing well!"
fi

# Expected progression timeline
echo ""
echo "📋 EXPECTED PROGRESSION TIMELINE"
echo "─────────────────────────────────────────────────────────────"
echo "  0-30 minutes:   Most agents reach 2 lasers on starter_mining"
echo "  30-60 minutes:  Agents start reaching mining_enhanced + 3 lasers"
echo "  60-120 minutes: Wealthy agents reach mining_barge + 4 lasers"
echo "  120+ minutes:   Very wealthy agents reach mining_cruiser + 6 lasers"
echo ""
echo "  Success Criteria:"
echo "    • <5% of agents still on starter_mining after 2 hours"
echo "    • >80% of mining_enhanced ships have 3 lasers"
echo "    • >80% of mining_barge ships have 4 lasers"
echo "    • >80% of mining_cruiser ships have 6 lasers"
echo ""

echo "═══════════════════════════════════════════════════════════"
echo "Run this script every 15-30 minutes to track progress"
echo "═══════════════════════════════════════════════════════════"
