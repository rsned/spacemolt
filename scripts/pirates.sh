#!/bin/bash

# Simple script to manage pirate agents

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

case "$1" in
  start)
    echo "🏴‍☠️ Starting all 5 pirate agents..."
    for i in {1..5}; do
      (cd "$SCRIPT_DIR" && ./bin/auto-miner $i > logs/pirate-$i.log 2>&1 &)
      echo "  Started Pirate $i (PID: $!)"
    done
    sleep 2
    COUNT=$(ps aux | grep auto-miner | grep -v grep | wc -l)
    echo "✅ $COUNT pirates running"
    ;;

  stop)
    echo "🛑 Stopping all pirate agents..."
    killall auto-miner 2>/dev/null
    sleep 1
    if ps aux | grep auto-miner | grep -v grep > /dev/null; then
      echo "⚠️  Some pirates still running, force killing..."
      killall -9 auto-miner 2>/dev/null
    fi
    echo "✅ All pirates stopped"
    ;;

  restart)
    echo "🔄 Restarting all pirates..."
    $0 stop
    sleep 2
    $0 start
    ;;

  status)
    COUNT=$(ps aux | grep auto-miner | grep -v grep | wc -l)
    if [ $COUNT -eq 0 ]; then
      echo "❌ No pirates running"
    else
      echo "✅ $COUNT pirates running:"
      ps aux | grep auto-miner | grep -v grep | awk '{print "  Pirate "$NF" (PID: "$2")"}'
    fi
    ;;

  watch)
    echo "👀 Watching all pirate activity (Ctrl+C to exit)..."
    tail -f logs/pirate-*.log
    ;;

  summary)
    echo "📊 PIRATE CREW SUMMARY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    for i in {1..5}; do
      if [ -f "logs/pirate-$i.log" ]; then
        echo ""
        echo "PIRATE-$i:"

        # Latest credits
        CREDITS=$(tail -100 logs/pirate-$i.log 2>/dev/null | grep "Current Credits:" | tail -1)
        if [ -n "$CREDITS" ]; then
          echo "  $CREDITS"
        else
          echo "  No credit info available"
        fi

        # Latest upgrade
        UPGRADE=$(tail -100 logs/pirate-$i.log 2>/dev/null | grep "✅ UPGRADED" | tail -1)
        if [ -n "$UPGRADE" ]; then
          echo "  $UPGRADE"
        fi

        # Latest run
        RUN=$(tail -50 logs/pirate-$i.log 2>/dev/null | grep "═══ Mining Run" | tail -1)
        if [ -n "$RUN" ]; then
          echo "  $RUN"
        fi
      else
        echo ""
        echo "PIRATE-$i: No log file found"
      fi
      echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    done
    ;;

  upgrades)
    echo "💎 UPGRADE HISTORY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    grep -h "✅ UPGRADED\|⛏️  Buying\|📦 Buying\|🛡️  Buying\|🔫 Buying" logs/pirate-*.log 2>/dev/null | tail -20
    if [ $? -ne 0 ]; then
      echo "No upgrades found yet"
    fi
    ;;

  tail)
    if [ -z "$2" ]; then
      echo "Usage: $0 tail <pirate-number>"
      echo "Example: $0 tail 1"
      exit 1
    fi
    echo "📜 Watching Pirate $2 (Ctrl+C to exit)..."
    tail -f logs/pirate-$2.log
    ;;

  rebuild)
    echo "🔨 Rebuilding auto-miner..."
    go build -o bin/auto-miner cmd/auto-miner/main.go
    if [ $? -eq 0 ]; then
      echo "✅ Build successful"
      echo ""
      read -p "Restart pirates with new version? (y/n) " -n 1 -r
      echo
      if [[ $REPLY =~ ^[Yy]$ ]]; then
        $0 restart
      fi
    else
      echo "❌ Build failed"
      exit 1
    fi
    ;;

  *)
    echo "🏴‍☠️ Pirate Agent Manager"
    echo ""
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  start      - Start all 5 pirate agents"
    echo "  stop       - Stop all pirate agents"
    echo "  restart    - Restart all pirate agents"
    echo "  status     - Check if pirates are running"
    echo "  watch      - Watch live logs from all pirates"
    echo "  tail <N>   - Watch live logs from pirate N"
    echo "  summary    - Show current status of all pirates"
    echo "  upgrades   - Show recent upgrade purchases"
    echo "  rebuild    - Rebuild the auto-miner binary"
    echo ""
    echo "Examples:"
    echo "  $0 start"
    echo "  $0 status"
    echo "  $0 summary"
    echo "  $0 tail 1"
    exit 1
    ;;
esac
