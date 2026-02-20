#!/bin/bash
# show_scrape_diffs.sh - Compare IDs between two JSON scrape files
# Usage: show_scrape_diffs.sh <old_file.json> <new_file.json>
#
# Shows which IDs have been added or removed between two scrape files
# Works with any JSON files containing arrays of objects with "id" fields

set -e

if [ $# -ne 2 ]; then
    echo "Usage: $0 <old_file.json> <new_file.json>"
    echo ""
    echo "Shows diffs in 'id' fields between two JSON scrape files."
    echo ""
    echo "Examples:"
    echo "  $0 old/get_skills.json new/get_skills.json"
    echo "  $0 data/old/catalog_recipes.json data/new/catalog_recipes.json"
    echo ""
    echo "Output format (similar to git diff):"
    echo "  12a13,14    - Lines 12 was removed, lines 13-14 were added after line 12"
    echo "  22a25       - Line 22 was removed, line 25 was added after line 22"
    echo "  60a64       - Line 60 was removed, line 64 was added after line 22"
    echo "  > \"id\": \"new_id\" - Indicates added IDs (green in git diff)"
    echo "  < \"id\": \"old_id\" - Indicates removed IDs (red in git diff)"
    exit 1
fi

OLD_FILE="$1"
NEW_FILE="$2"

# Check if files exist
if [ ! -f "$OLD_FILE" ]; then
    echo "Error: Old file not found: $OLD_FILE"
    exit 1
fi

if [ ! -f "$NEW_FILE" ]; then
    echo "Error: New file not found: $NEW_FILE"
    exit 1
fi

# Temporary files for extracted IDs
OLD_IDS=$(mktemp)
NEW_IDS=$(mktemp)
OLD_IDS_SORTED=$(mktemp)
NEW_IDS_SORTED=$(mktemp)

# Cleanup on exit
trap "rm -f $OLD_IDS $NEW_IDS $OLD_IDS_SORTED $NEW_IDS_SORTED" EXIT

# Extract IDs from old file
# Handle multiple formats:
# 1. {"items": [{"id": "xxx"}, ...]}
# 2. {"recipes": {"recipe_id": {"id": "recipe_id"}, ...}}
# 3. [{"id": "xxx"}, ...]
if jq -e '.items' "$OLD_FILE" > /dev/null 2>&1; then
    # Format 1: Has an "items" array
    jq -r '.items | .[].id // empty' "$OLD_FILE" > "$OLD_IDS"
elif jq -e '.recipes' "$OLD_FILE" > /dev/null 2>&1; then
    # Format 2: Has a "recipes" object (keys are recipe IDs)
    jq -r '.recipes | keys[]' "$OLD_FILE" > "$OLD_IDS"
elif jq -e 'if type=="array" then . else .items end' "$OLD_FILE" > /dev/null 2>&1; then
    # Format 3: Is a simple array or has items array
    jq -r 'if type=="array" then . else .items end | .[].id // empty' "$OLD_FILE" > "$OLD_IDS"
else
    # Fallback: Try to extract IDs from any array in the file
    jq -r '.. | objects | .id // empty' "$OLD_FILE" | grep -v '^$' > "$OLD_IDS"
fi

# Extract IDs from new file
if jq -e '.items' "$NEW_FILE" > /dev/null 2>&1; then
    # Format 1: Has an "items" array
    jq -r '.items | .[].id // empty' "$NEW_FILE" > "$NEW_IDS"
elif jq -e '.recipes' "$NEW_FILE" > /dev/null 2>&1; then
    # Format 2: Has a "recipes" object (keys are recipe IDs)
    jq -r '.recipes | keys[]' "$NEW_FILE" > "$NEW_IDS"
elif jq -e 'if type=="array" then . else .items end' "$NEW_FILE" > /dev/null 2>&1; then
    # Format 3: Is a simple array or has items array
    jq -r 'if type=="array" then . else .items end | .[].id // empty' "$NEW_FILE" > "$NEW_IDS"
else
    # Fallback: Try to extract IDs from any array in the file
    jq -r '.. | objects | .id // empty' "$NEW_FILE" | grep -v '^$' > "$NEW_IDS"
fi

# Check if we got any IDs
if [ ! -s "$OLD_IDS" ]; then
    echo "Warning: No IDs found in old file: $OLD_FILE"
    exit 1
fi

if [ ! -s "$NEW_IDS" ]; then
    echo "Warning: No IDs found in new file: $NEW_FILE"
    exit 1
fi

# Sort and number the IDs for diff
nl -w3 -s' ' < "$OLD_IDS" > "$OLD_IDS_SORTED"
nl -w3 -s' ' < "$NEW_IDS" > "$NEW_IDS_SORTED"

# Count stats
OLD_COUNT=$(wc -l < "$OLD_IDS")
NEW_COUNT=$(wc -l < "$NEW_IDS")

echo "Comparing IDs between:"
echo "  Old: $OLD_FILE ($OLD_COUNT IDs)"
echo "  New: $NEW_FILE ($NEW_COUNT IDs)"
echo ""

# Check if there are differences using comm
ADDED=$(comm -13 <(sort "$OLD_IDS") <(sort "$NEW_IDS"))
REMOVED=$(comm -23 <(sort "$OLD_IDS") <(sort "$NEW_IDS"))

# Show summary
if [ -z "$ADDED" ] && [ -z "$REMOVED" ]; then
    echo "✅ No differences found - all IDs match"
    exit 0
fi

# Calculate counts
ADDED_COUNT=0
REMOVED_COUNT=0
[ -n "$ADDED" ] && ADDED_COUNT=$(echo "$ADDED" | wc -l)
[ -n "$REMOVED" ] && REMOVED_COUNT=$(echo "$REMOVED" | wc -l)

if [ $ADDED_COUNT -gt 0 ] && [ $REMOVED_COUNT -gt 0 ]; then
    echo "Summary: $REMOVED_COUNT removed, $ADDED_COUNT added"
elif [ $ADDED_COUNT -gt 0 ]; then
    echo "Summary: $NEW_COUNT IDs (+$ADDED_COUNT added)"
elif [ $REMOVED_COUNT -gt 0 ]; then
    echo "Summary: $NEW_COUNT IDs (-$REMOVED_COUNT removed)"
fi
echo ""

# Create numbered versions for diff
OLD_NUMBERED=$(mktemp)
NEW_NUMBERED=$(mktemp)
trap "rm -f $OLD_IDS $NEW_IDS $OLD_IDS_SORTED $NEW_IDS_SORTED $OLD_NUMBERED $NEW_NUMBERED" EXIT

sort "$OLD_IDS" | nl -w3 -s' ' > "$OLD_NUMBERED"
sort "$NEW_IDS" | nl -w3 -s' ' > "$NEW_NUMBERED"

# Run diff to show what changed
if diff --unified=0 "$OLD_NUMBERED" "$NEW_NUMBERED" > /dev/null 2>&1; then
    # No differences
    echo "✅ No differences found in IDs"
else
    # There are differences - show them
    diff --unified=0 "$OLD_NUMBERED" "$NEW_NUMBERED" | \
        awk '
            /^@@/ {
                # Show hunk headers
                print
                next
            }
            /^> / {
                # Added line - format with green +
                gsub(/^> [0-9]+\t/, "> ")
                gsub(/"/, "\\\"")
                print
                next
            }
            /^< / {
                # Removed line - format with red -
                gsub(/^< [0-9]+\t/, "< ")
                gsub(/"/, "\\\"")
                print
                next
            }
        '
fi

echo ""

# Show detailed breakdown
echo "Detailed breakdown:"

if [ -n "$REMOVED" ]; then
    REMOVED_COUNT=$(echo "$REMOVED" | wc -l)
    echo "  Removed IDs ($REMOVED_COUNT):"
    echo "$REMOVED" | while read id; do
        echo "    - $id"
    done
fi

if [ -n "$ADDED" ]; then
    ADDED_COUNT=$(echo "$ADDED" | wc -l)
    echo "  Added IDs ($ADDED_COUNT):"
    echo "$ADDED" | while read id; do
        echo "    + $id"
    done
fi

# Show common IDs
COMMON=$(comm -12 <(sort "$OLD_IDS") <(sort "$NEW_IDS"))
if [ -n "$COMMON" ]; then
    COMMON_COUNT=$(echo "$COMMON" | wc -l)
    echo "  Unchanged IDs: $COMMON_COUNT"
fi

# Show detailed stats if there were changes
if [ $? -ne 0 ] || [ $OLD_COUNT -ne $NEW_COUNT ]; then
    echo ""
    echo "Detailed breakdown:"

    # Show added IDs
    ADDED=$(comm -13 <(sort "$OLD_IDS") <(sort "$NEW_IDS"))
    if [ -n "$ADDED" ]; then
        ADDED_COUNT=$(echo "$ADDED" | wc -l)
        echo "  Added IDs: $ADDED_COUNT"
        # Show first 10 added IDs
        echo "$ADDED" | head -10 | while read id; do
            echo "    + $id"
        done
        if [ $ADDED_COUNT -gt 10 ]; then
            echo "    ... and $((ADDED_COUNT - 10)) more"
        fi
    fi

    # Show removed IDs
    REMOVED=$(comm -23 <(sort "$OLD_IDS") <(sort "$NEW_IDS"))
    if [ -n "$REMOVED" ]; then
        REMOVED_COUNT=$(echo "$REMOVED" | wc -l)
        echo "  Removed IDs: $REMOVED_COUNT"
        # Show first 10 removed IDs
        echo "$REMOVED" | head -10 | while read id; do
            echo "    - $id"
        done
        if [ $REMOVED_COUNT -gt 10 ]; then
            echo "    ... and $((REMOVED_COUNT - 10)) more"
        fi
    fi

    # Show common IDs
    COMMON=$(comm -12 <(sort "$OLD_IDS") <(sort "$NEW_IDS"))
    if [ -n "$COMMON" ]; then
        COMMON_COUNT=$(echo "$COMMON" | wc -l)
        echo "  Unchanged IDs: $COMMON_COUNT"
    fi
fi
