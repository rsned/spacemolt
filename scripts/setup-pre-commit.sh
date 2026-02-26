#!/bin/bash
# Setup script for git pre-commit hook
# Run this script to install the pre-commit hook: ./scripts/setup-pre-commit.sh

set -e

HOOK_DIR=".git/hooks"
HOOK_FILE="$HOOK_DIR/pre-commit"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Setting up pre-commit hook..."

# Create hooks directory if it doesn't exist
if [ ! -d "$HOOK_DIR" ]; then
    mkdir -p "$HOOK_DIR"
    echo "Created hooks directory: $HOOK_DIR"
fi

# Create the pre-commit hook
cat > "$HOOK_FILE" << 'EOF'
#!/bin/bash
# Pre-commit hook: Run build, tests with race detection, and lint on Go code
# Skip with: git commit --no-verify

echo "Running pre-commit checks..."

# Run quick race detection on modified packages
CHANGED_GO=$(git diff --cached --name-only | grep '\.go$' || true)

if [ -z "$CHANGED_GO" ]; then
    echo "No Go files staged, skipping checks"
    exit 0
fi

# Extract unique package paths from changed files
PACKAGES=$(echo "$CHANGED_GO" | xargs dirname | sort -u | sed 's|^|./|')

FAILED=0

# Phase 1: Build check
echo "[1/3] Checking build..."
if ! go build ./... 2>&1; then
    echo "  ✗ Build failed"
    exit 1
fi
echo "  ✓ Build passed"

# Phase 2: Race detector tests on changed packages
echo "[2/3] Running tests with race detector..."
for pkg in $PACKAGES; do
    if [ -d "${pkg#./}" ]; then
        echo "  Testing $pkg..."
        if ! go test -race -count=1 -timeout=120s "$pkg" 2>&1; then
            echo "  ✗ Tests failed for $pkg"
            FAILED=1
        fi
    fi
done

# Phase 3: Lint
echo "[3/3] Running golangci-lint..."
if command -v golangci-lint &> /dev/null; then
    if ! golangci-lint run $PACKAGES 2>&1; then
        echo "  ✗ Lint findings detected"
        FAILED=1
    else
        echo "  ✓ Lint passed"
    fi
else
    echo "  ⚠ golangci-lint not installed, skipping"
fi

if [ $FAILED -eq 1 ]; then
    echo ""
    echo "✗ Pre-commit checks failed!"
    echo "Use 'git commit --no-verify' to bypass (not recommended)"
    exit 1
fi

echo "✓ Pre-commit checks passed"
exit 0
EOF

# Make it executable
chmod +x "$HOOK_FILE"

echo "✓ Pre-commit hook installed: $HOOK_FILE"
echo ""
echo "The hook will now run race detection before each commit."
echo "To bypass: git commit --no-verify"
