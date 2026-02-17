#!/bin/bash

# Build script for spacemolt-agent-server tools
# Builds all commands in ./cmd/ into ./bin/

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Create bin directory if it doesn't exist
mkdir -p bin

echo "Building spacemolt-agent-server tools..."
echo ""

# Track success/failure
SUCCESS_COUNT=0
FAIL_COUNT=0
FAILED_TOOLS=()

# Build each tool in cmd/
for dir in cmd/*/; do
    if [ -d "$dir" ]; then
        tool=$(basename "$dir")
        echo -n "Building $tool... "

        if go build -race -o "bin/$tool" "./$dir"; then
            echo -e "${GREEN}✓${NC}"
            ((SUCCESS_COUNT++))
        else
            echo -e "${RED}✗${NC}"
            ((FAIL_COUNT++))
            FAILED_TOOLS+=("$tool")
        fi
    fi
done

echo ""
echo "Build complete: $SUCCESS_COUNT succeeded, $FAIL_COUNT failed"

if [ $FAIL_COUNT -gt 0 ]; then
    echo -e "${RED}Failed tools:${NC}"
    for tool in "${FAILED_TOOLS[@]}"; do
        echo "  - $tool"
    done
    exit 1
fi

echo -e "${GREEN}All tools built successfully!${NC}"
echo "Binaries are in ./bin/"
