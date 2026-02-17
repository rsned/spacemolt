#!/bin/bash
# Test script for race detection and code quality checks
# Enhanced with better error handling and reporting

set -e

echo "================================"
echo "Running Go Race Detector Tests"
echo "================================"
echo ""

# Track if any tests failed
FAILED=0

# Run tests with race detector
echo "Running all tests with -race flag..."
if go test -race -timeout=5m ./...; then
    echo "✓ Race detector tests passed"
else
    echo "✗ Race detector tests failed"
    FAILED=1
fi

echo ""
echo "================================"
echo "Running golangci-lint"
echo "================================"
echo ""

# Run golangci-lint if available
if command -v golangci-lint &> /dev/null; then
    if golangci-lint run ./...; then
        echo "✓ golangci-lint passed"
    else
        echo "✗ golangci-lint failed"
        FAILED=1
    fi
else
    echo "⚠ golangci-lint not found. Install with:"
    echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$(go env GOPATH)/bin"
    echo "  Skipping linter checks..."
fi

echo ""
echo "================================"
echo "Running go vet"
echo "================================"
echo ""

# Run go vet
if go vet ./...; then
    echo "✓ go vet passed"
else
    echo "✗ go vet failed"
    FAILED=1
fi

echo ""
echo "================================"
echo "Checking for common concurrency issues"
echo "================================"
echo ""

# Look for potential race conditions in code
echo "Searching for potentially unsafe concurrent access patterns..."

# Find unprotected map access
UNPROTECTED_MAPS=$(grep -r "map\[.*\].*=" --include="*.go" . 2>/dev/null | grep -v "_test.go" | grep -v "mu.Lock" | grep -v "sync.Map" | wc -l)
echo -n "Unprotected map writes: $UNPROTECTED_MAPS"
if [ "$UNPROTECTED_MAPS" -gt 0 ]; then
    echo " (⚠ warning)"
else
    echo ""
fi

# Find goroutines without context
NO_CONTEXT=$(grep -r "go func()" --include="*.go" . 2>/dev/null | grep -v "ctx" | wc -l)
echo -n "Goroutines potentially missing context: $NO_CONTEXT"
if [ "$NO_CONTEXT" -gt 0 ]; then
    echo " (⚠ warning)"
else
    echo ""
fi

echo ""
echo "================================"
if [ $FAILED -eq 0 ]; then
    echo "✓ All checks passed!"
    echo "================================"
    exit 0
else
    echo "✗ Some checks failed!"
    echo "================================"
    exit 1
fi
