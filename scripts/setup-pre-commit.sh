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
# Pre-commit hook: Run race detection on Go code before commit
# Skip with: git commit --no-verify

echo "Running pre-commit checks..."

# Run quick race detection on modified packages
CHANGED_GO=$(git diff --cached --name-only | grep '\.go$' || true)

if [ -z "$CHANGED_GO" ]; then
    echo "No Go files staged, skipping race detection"
    exit 0
fi

echo "Checking modified Go files for race conditions..."

# Extract package paths from changed files
PACKAGES=$(echo "$CHANGED_GO" | xargs dirname | sort -u | sed 's|^|./|')

# Run race detector on changed packages
FAILED=0
for pkg in $PACKAGES; do
    echo "Testing $pkg..."
    if ! go test -race -timeout=30s "$pkg" 2>/dev/null; then
        echo "⚠ Race detection failed for $pkg"
        FAILED=1
    fi
done

# Run golangci-lint on changed files
if command -v golangci-lint &> /dev/null; then
    echo "Running golangci-lint on staged files..."
    if ! echo "$CHANGED_GO" | xargs -I{} golangci-lint run "{}" 2>/dev/null; then
        echo "⚠ Linting failed"
        FAILED=1
    fi
fi

if [ $FAILED -eq 1 ]; then
    echo ""
    echo "⚠ Pre-commit checks failed!"
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
