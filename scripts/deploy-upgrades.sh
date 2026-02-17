#!/bin/bash

# Deploy updated auto-miner to all agents
# Run this from the spacemolt-agent-server directory

set -e

echo "═══════════════════════════════════════════════════════════"
echo "AUTO-MINER UPGRADE DEPLOYMENT"
echo "═══════════════════════════════════════════════════════════"
echo ""

# Step 1: Build the updated binary
echo "📦 Step 1: Building updated auto-miner binary..."
go build -o auto-miner cmd/auto-miner/main.go
if [ $? -eq 0 ]; then
    echo "✅ Build successful!"
else
    echo "❌ Build failed! Aborting deployment."
    exit 1
fi

echo ""

# Step 2: Check for running agents
echo "🔍 Step 2: Checking for running agents..."
running_count=$(pgrep -f "auto-miner" | wc -l)
echo "Found $running_count running auto-miner processes"

if [ "$running_count" -gt 0 ]; then
    echo ""
    read -p "Stop all running agents? (y/n): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "🛑 Stopping all auto-miner agents..."
        pkill -f "auto-miner"
        sleep 3
        echo "✅ All agents stopped"
    else
        echo "❌ Deployment cancelled - agents still running"
        echo "Please stop agents manually before deploying new binary"
        exit 1
    fi
fi

echo ""

# Step 3: Verify the build
echo "✓ Step 3: Verifying build..."
if [ -f "./auto-miner" ]; then
    size=$(stat -c%s "./auto-miner" 2>/dev/null || stat -f%z "./auto-miner")
    echo "Binary size: $size bytes"
    echo "✅ Binary ready for deployment"
else
    echo "❌ Binary not found! Build may have failed."
    exit 1
fi

echo ""

# Step 4: Deployment instructions
echo "═══════════════════════════════════════════════════════════"
echo "DEPLOYMENT COMPLETE!"
echo "═══════════════════════════════════════════════════════════"
echo ""
echo "The updated auto-miner binary is ready at: ./auto-miner"
echo ""
echo "📋 NEXT STEPS:"
echo ""
echo "1. Restart your agents using your usual startup script"
echo "   Example: ./start-all-agents.sh"
echo "   Or: ./launch-agents.sh"
echo ""
echo "2. Monitor progress using the monitoring script:"
echo "   ./monitor-upgrades.sh"
echo ""
echo "   Run this every 15-30 minutes to track upgrade progress"
echo ""
echo "3. Expected timeline:"
echo "   • 0-30 min:  Most agents reach 2 lasers on starter_mining"
echo "   • 30-60 min: Agents start reaching mining_enhanced"
echo "   • 60-120 min: Wealthy agents reach mining_barge"
echo "   • 120+ min: Very wealthy agents reach mining_cruiser"
echo ""
echo "4. Success criteria after 2 hours:"
echo "   • <5% of agents still on starter_mining"
echo "   • >80% of mining_enhanced ships have 3 lasers"
echo "   • >80% of mining_barge ships have 4 lasers"
echo "   • >80% of mining_cruiser ships have 6 lasers"
echo ""
echo "═══════════════════════════════════════════════════════════"
echo "KEY CHANGES IN THIS VERSION:"
echo "═══════════════════════════════════════════════════════════"
echo ""
echo "✓ NEW: Added mining_cruiser tier (25000cr, 6 lasers)"
echo "✓ SIMPLIFIED: Pure mining focus - removed weapons/shields/cargo"
echo "✓ Fixed priority order (ship upgrades before lasers)"
echo "✓ Increased upgrade check frequency (every 3 runs vs 5)"
echo "✓ Better market listing synchronization (5s vs 2s wait)"
echo "✓ Improved state sync after ship purchases (5s vs 3s)"
echo "✓ Cleaner code with unused functions removed"
echo ""
echo "All credits now go toward mining efficiency ONLY."
echo "Maximum income generation through optimized mining."
echo ""
echo "═══════════════════════════════════════════════════════════"
echo ""

# Optional: Show diff summary
if [ -d ".git" ]; then
    echo "📊 CHANGES SUMMARY:"
    echo "─────────────────────────────────────────────────────────────"
    git diff --stat cmd/auto-miner/main.go 2>/dev/null || echo "(Git diff unavailable)"
    echo ""
fi
