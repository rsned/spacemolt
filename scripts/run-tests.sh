#!/bin/bash
# Comprehensive test runner with race detection and parallel execution
# Usage: ./scripts/run-tests.sh [package-pattern]
# Examples:
#   ./scripts/run-tests.sh              # Run all tests
#   ./scripts/run-tests.sh ./pkg/game/  # Run only game package tests

set -euo pipefail

PACKAGE="${1:-./...}"
TIMEOUT="${TEST_TIMEOUT:-5m}"
PARALLEL="${TEST_PARALLEL:-4}"
FAILED=0
START_TIME=$(date +%s)

echo "========================================"
echo " Spacemolt Test Suite"
echo "========================================"
echo " Package:  $PACKAGE"
echo " Timeout:  $TIMEOUT"
echo " Parallel: $PARALLEL"
echo "========================================"
echo ""

# Phase 1: Build check
echo "[1/4] Checking build..."
if go build $PACKAGE 2>&1; then
    echo "  ✓ Build passed"
else
    echo "  ✗ Build failed"
    exit 1
fi
echo ""

# Phase 2: Race detector tests
echo "[2/4] Running tests with race detector (parallel=$PARALLEL)..."
if go test -race -count=1 -parallel="$PARALLEL" -timeout="$TIMEOUT" $PACKAGE 2>&1; then
    echo "  ✓ All tests passed with race detector"
else
    echo "  ✗ Tests failed"
    FAILED=1
fi
echo ""

# Phase 3: Coverage report
echo "[3/4] Generating coverage report..."
COVERAGE_FILE=$(mktemp /tmp/spacemolt-coverage-XXXXXX.out)
if go test -coverprofile="$COVERAGE_FILE" -timeout="$TIMEOUT" $PACKAGE 2>&1; then
    echo ""
    echo "  Coverage summary:"
    go tool cover -func="$COVERAGE_FILE" | tail -1
else
    echo "  ✗ Coverage generation failed"
    FAILED=1
fi
rm -f "$COVERAGE_FILE"
echo ""

# Phase 4: Lint
echo "[4/4] Running golangci-lint..."
if command -v golangci-lint &> /dev/null; then
    if golangci-lint run $PACKAGE 2>&1; then
        echo "  ✓ Lint passed"
    else
        echo "  ✗ Lint findings detected"
        FAILED=1
    fi
else
    echo "  ⚠ golangci-lint not installed, skipping"
fi

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo ""
echo "========================================"
echo " Results (${ELAPSED}s elapsed)"
echo "========================================"
if [ $FAILED -eq 0 ]; then
    echo " ✓ All checks passed!"
    exit 0
else
    echo " ✗ Some checks failed!"
    exit 1
fi
