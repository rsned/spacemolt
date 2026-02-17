#!/bin/bash
# Quick test script for the crafting MCP server

DB_PATH="${DB_PATH:-data/crafting/crafting.db}"
SERVER="./bin/crafting-server"

if [ ! -f "$SERVER" ]; then
    echo "Error: Server binary not found at $SERVER"
    echo "Run: go build -o bin/crafting-server ./cmd/crafting-server"
    exit 1
fi

if [ ! -f "$DB_PATH" ]; then
    echo "Error: Database not found at $DB_PATH"
    exit 1
fi

echo "🧪 Testing SpaceMolt Crafting MCP Server"
echo "========================================"
echo ""

# Test 1: Initialize
echo "📋 Test 1: Initialize"
INIT_RESULT=$(echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | $SERVER -db $DB_PATH 2>/dev/null)
echo "$INIT_RESULT" | jq -r '.result.serverInfo.name'
echo ""

# Test 2: List tools
echo "🔧 Test 2: List Tools"
TOOLS_RESULT=$(echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | $SERVER -db $DB_PATH 2>/dev/null)
TOOL_COUNT=$(echo "$TOOLS_RESULT" | jq -r '.result.tools | length')
echo "Found $TOOL_COUNT tools:"
echo "$TOOLS_RESULT" | jq -r '.result.tools[].name' | sed 's/^/  - /'
echo ""

# Test 3: craft_query
echo "⚒️  Test 3: craft_query (50 copper ore, 30 iron ore)"
QUERY_RESULT=$(echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"craft_query","arguments":{"components":[{"id":"ore_copper","quantity":50},{"id":"ore_iron","quantity":30}],"skills":{"crafting_basic":1},"limit":5}}}' | $SERVER -db $DB_PATH 2>/dev/null)

CRAFTABLE=$(echo "$QUERY_RESULT" | jq -r '.result.content[0].text' | jq -r '.craftable | length')
BLOCKED=$(echo "$QUERY_RESULT" | jq -r '.result.content[0].text' | jq -r '.blocked_by_skills | length')
PROCESSING_MS=$(echo "$QUERY_RESULT" | jq -r '.result.content[0].text' | jq -r '.query_stats.processing_time_ms')

echo "Results:"
echo "  - Craftable recipes: $CRAFTABLE"
echo "  - Blocked by skills: $BLOCKED"
echo "  - Processing time: ${PROCESSING_MS}ms"
echo ""

if [ "$CRAFTABLE" -gt 0 ]; then
    echo "Craftable recipes:"
    echo "$QUERY_RESULT" | jq -r '.result.content[0].text' | jq -r '.craftable[].recipe | "  - \(.name) (\(.id)) - craft \(.output.quantity)x \(.output.item_id)"'
fi
echo ""

# Test 4: skill_craft_paths
echo "📚 Test 4: skill_craft_paths (crafting_basic:1)"
SKILL_RESULT=$(echo '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"skill_craft_paths","arguments":{"skills":{"crafting_basic":{"level":1,"current_xp":50}},"limit":3}}}' | $SERVER -db $DB_PATH 2>/dev/null)

echo "Top 3 skill progression paths:"
echo "$SKILL_RESULT" | jq -r '.result.content[0].text' | jq -r '.skill_paths[] | "  - \(.skill.name) (level \(.current_level) -> \(.current_level + 1)): \(.recipes_unlocked_at_next | length) recipes unlocked with \(.xp_to_next_level) XP"'
echo ""

# Test 5: recipe_lookup
echo "🔍 Test 5: recipe_lookup (basic_copper_processing)"
RECIPE_RESULT=$(echo '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"recipe_lookup","arguments":{"recipe_id":"basic_copper_processing"}}}' | $SERVER -db $DB_PATH 2>/dev/null)

RECIPE_NAME=$(echo "$RECIPE_RESULT" | jq -r '.result.content[0].text' | jq -r '.recipe.name')
RECIPE_DESC=$(echo "$RECIPE_RESULT" | jq -r '.result.content[0].text' | jq -r '.recipe.description')
echo "Recipe: $RECIPE_NAME"
echo "  $RECIPE_DESC"
echo ""

echo "✅ All tests completed!"
