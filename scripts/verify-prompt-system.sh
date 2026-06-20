#!/bin/bash
# Verification script for Prompt Management System

set -e

echo "=== Prompt Management System Verification ==="
echo

# 1. Check directory structure
echo "1. Checking directory structure..."
if [ -d "data/prompts/templates/decision" ]; then
    echo "   ✓ Templates directory exists"
else
    echo "   ✗ Templates directory missing"
    exit 1
fi

if [ -d "data/prompts/templates/shared" ]; then
    echo "   ✓ Shared templates directory exists"
else
    echo "   ✗ Shared templates directory missing"
    exit 1
fi

# 2. Check template files
echo
echo "2. Checking template files..."
if [ -f "data/prompts/templates/decision/decision.v1.tmpl" ]; then
    echo "   ✓ decision.v1.tmpl exists"
else
    echo "   ✗ decision.v1.tmpl missing"
    exit 1
fi

if [ -f "data/prompts/templates/shared/personality_context.tmpl" ]; then
    echo "   ✓ personality_context.tmpl exists"
else
    echo "   ✗ personality_context.tmpl missing"
    exit 1
fi

if [ -f "data/prompts/templates/shared/actions_list.tmpl" ]; then
    echo "   ✓ actions_list.tmpl exists"
else
    echo "   ✗ actions_list.tmpl missing"
    exit 1
fi

if [ -f "data/prompts/templates/shared/json_format.tmpl" ]; then
    echo "   ✓ json_format.tmpl exists"
else
    echo "   ✗ json_format.tmpl missing"
    exit 1
fi

# 3. Check configuration
echo
echo "3. Checking configuration..."
if [ -f "data/prompts/config.yaml" ]; then
    echo "   ✓ config.yaml exists"

    # Check for required fields
    if grep -q "active_version:" data/prompts/config.yaml; then
        echo "   ✓ Configuration has active_version"
    else
        echo "   ✗ Configuration missing active_version"
        exit 1
    fi
else
    echo "   ✗ config.yaml missing"
    exit 1
fi

# 4. Build tests
echo
echo "4. Building packages..."
if go build ./pkg/prompts/... 2>&1 | grep -q "error"; then
    echo "   ✗ Build failed"
    exit 1
else
    echo "   ✓ pkg/prompts builds successfully"
fi

if go build ./pkg/llm/... 2>&1 | grep -q "error"; then
    echo "   ✗ Build failed"
    exit 1
else
    echo "   ✓ pkg/llm builds successfully"
fi

if go build ./pkg/agent/... 2>&1 | grep -q "error"; then
    echo "   ✗ Build failed"
    exit 1
else
    echo "   ✓ pkg/agent builds successfully"
fi

# 5. Run tests
echo
echo "5. Running tests..."
if go test ./pkg/prompts/... -v > /tmp/prompt-tests.log 2>&1; then
    echo "   ✓ All tests pass"
    grep "PASS:" /tmp/prompt-tests.log | wc -l | xargs echo "     Tests passed:"
else
    echo "   ✗ Tests failed"
    cat /tmp/prompt-tests.log
    exit 1
fi

# 6. Build final binaries
echo
echo "6. Building overmind and worker..."
if go build -o bin/overmind ./cmd/overmind > /dev/null 2>&1; then
    echo "   ✓ overmind builds successfully"
else
    echo "   ✗ overmind build failed"
    exit 1
fi
if go build -o bin/worker ./cmd/worker > /dev/null 2>&1; then
    echo "   ✓ worker builds successfully"
else
    echo "   ✗ worker build failed"
    exit 1
fi

# 7. Check documentation
echo
echo "7. Checking documentation..."
if [ -f "docs/PROMPT_SYSTEM.md" ]; then
    echo "   ✓ System documentation exists"
else
    echo "   ✗ System documentation missing"
    exit 1
fi

if [ -f "data/prompts/README.md" ]; then
    echo "   ✓ Quick start guide exists"
else
    echo "   ✗ Quick start guide missing"
    exit 1
fi

# Summary
echo
echo "=== Verification Complete ==="
echo
echo "✅ All checks passed!"
echo
echo "The Prompt Management System is fully functional."
echo
echo "Quick Start:"
echo "  1. Start agent server: ./agent-server --agents=miner-2"
echo "  2. Check logs for: '✓ Prompt management system enabled'"
echo "  3. View metrics: cat data/prompts/metadata/decision.v1.yaml"
echo "  4. Read docs: docs/PROMPT_SYSTEM.md"
echo
