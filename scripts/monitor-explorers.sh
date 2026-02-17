#!/bin/bash

# Monitor all explorers
while true; do
  clear
  echo "╔════════════════════════════════════════════════════════════════════════════╗"
  echo "║                    EXPLORER FLEET MONITORING DASHBOARD                     ║"
  echo "╚════════════════════════════════════════════════════════════════════════════╝"
  echo ""
  date
  echo ""

  # Count running explorers
  running=$(ps aux | grep "auto-explorer [0-9]" | grep -v grep | wc -l)
  echo "Active Explorers: $running/10"
  echo ""
  echo "────────────────────────────────────────────────────────────────────────────"

  for i in {1..10}; do
    # Check if explorer is running
    if ps aux | grep "auto-explorer $i" | grep -v grep > /dev/null; then
      status="✓ RUNNING"
    else
      status="✗ STOPPED"
      echo "Explorer-$i: $status"
      continue
    fi

    # Get latest activity
    log_file="logs/explorer-$i.log"

    # Check for phase
    phase="Phase 1"
    if grep -q "PHASE 2: EXPLORATION" "$log_file" 2>/dev/null; then
      phase="Phase 2"
    fi

    # Get credits
    credits=$(tail -200 "$log_file" 2>/dev/null | grep "Credits:" | tail -1 | sed -n 's/.*Credits: \([0-9]*\).*/\1/p')
    if [ -z "$credits" ]; then
      credits=$(tail -100 "$log_file" 2>/dev/null | grep -o "credits\":[0-9]*" | tail -1 | cut -d: -f2)
    fi

    # Get latest action
    latest=$(tail -50 "$log_file" 2>/dev/null | grep -E "Mining Run|Exploring new system|Jumping to|Saved system data" | tail -1 | sed 's/.*EXPLORER-[0-9]* //' | cut -c 1-50)

    # Get mining runs or systems explored
    if [ "$phase" = "Phase 1" ]; then
      runs=$(tail -200 "$log_file" 2>/dev/null | grep -o "Mining Run #[0-9]*" | tail -1 | sed 's/Mining Run #//')
      metric="Run: $runs"
    else
      systems=$(tail -200 "$log_file" 2>/dev/null | grep "Saved system data:" | wc -l)
      metric="Systems: $systems"
    fi

    # Check for errors
    errors=$(tail -20 "$log_file" 2>/dev/null | grep -c "✗\|ERROR\|Failed")
    if [ "$errors" -gt 0 ]; then
      error_msg=" ⚠️ "
    else
      error_msg=""
    fi

    printf "Explorer-%-2s | %-7s | Credits: %-5s | %-12s | %s%s\n" \
      "$i" "$phase" "${credits:-0}" "$metric" "$latest" "$error_msg"
  done

  echo "────────────────────────────────────────────────────────────────────────────"
  echo ""
  echo "Press Ctrl+C to stop monitoring"
  echo "Refreshing in 10 seconds..."

  sleep 10
done
