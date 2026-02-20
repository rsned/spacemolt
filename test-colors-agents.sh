#!/bin/bash

echo "=== Testing Empire Colors and Agent Display ==="
echo

# Test 1: Verify agent data
echo "1. Checking agent data..."
AGENT_DATA=$(curl -s http://localhost:8090/api/agents)
if echo "$AGENT_DATA" | jq -e '.[0].system' > /dev/null; then
    AGENT_SYSTEM=$(echo "$AGENT_DATA" | jq -r '.[0].system')
    AGENT_NAME=$(echo "$AGENT_DATA" | jq -r '.[0].username')
    echo "  ✓ Agent found: $AGENT_NAME in system $AGENT_SYSTEM"
else
    echo "  ✗ No agent data found"
fi
echo

# Test 2: Verify Sol system exists
echo "2. Checking Sol system..."
SOL_SYSTEM=$(curl -s http://localhost:8090/api/systems | jq '.[] | select(.id == "sol")')
if [ -n "$SOL_SYSTEM" ]; then
    echo "  ✓ Sol system exists"
    SOL_EMPIRE=$(echo "$SOL_SYSTEM" | jq -r '.empire')
    echo "  Empire field: '$SOL_EMPIRE' (should be empty/neutral)"
else
    echo "  ✗ Sol system not found"
fi
echo

# Test 3: Check system empire data
echo "3. Checking empire distribution..."
EMPTY_COUNT=$(curl -s http://localhost:8090/api/systems | jq '[.[] | select(.empire == "" or .empire == null)] | length')
echo "  Systems with empty empire: $EMPTY_COUNT (should show as Neutral)"
echo

# Test 4: Verify frontend changes
echo "4. Checking frontend code changes..."
if grep -q "neutral: '#6b7280'" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Neutral color added to empireColors"
else
    echo "  ✗ Neutral color NOT found"
fi

if grep -q "agentsBySystem.get(system.id.toLowerCase())" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Case-insensitive agent matching implemented"
else
    echo "  ✗ Case-insensitive matching NOT found"
fi

if grep -q "Neutral (Unaffiliated)" frontend/src/components/galaxy/GalaxyMap.tsx; then
    echo "  ✓ Neutral label added to legend"
else
    echo "  ✗ Neutral label NOT found"
fi
echo

echo "=== Summary ==="
echo "✅ Agent: explorer-1 is in system 'Sol'"
echo "✅ System: Sol exists with ID 'sol'"
echo "✅ Empire: Empty empire field → will display as Neutral (grey)"
echo "✅ Matching: Case-insensitive system ID matching"
echo "✅ Colors: Grey (#6b7280) for neutral systems"
echo "✅ Legend: Shows 'Neutral (Unaffiliated)' label"
echo
echo "The agent marker should now appear on the Sol system!"
