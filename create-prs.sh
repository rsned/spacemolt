#!/bin/bash
set -eu

# Check for dry-run flag
DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN=true
    echo "🔍 DRY RUN MODE - No changes will be made"
    echo ""
fi

# Get list of commits from main to feature/agent-server in chronological order (oldest first)
# Format: hash|subject
mapfile -t commits < <(git log main..feature/agent-server --reverse --format="%H|%s")

echo "Found ${#commits[@]} commits to process"
echo ""

declare -i processed=0
declare -i skipped=0

for commit_line in "${commits[@]}"; do
    IFS='|' read -r hash subject <<< "$commit_line"

    # Skip merge commits
    if [[ "$subject" =~ ^Merge ]]; then
        echo "⏭️  Skipping merge commit: $hash - $subject"
        echo ""
        skipped=$((skipped + 1))
        continue
    fi

    # Create branch name from subject
    # Remove commit type prefix (feat:, fix:, chore:, etc.)
    branch_name=$(echo "$subject" | sed -E 's/^(feat|fix|chore|refactor|docs|test|style|perf|ci|build|revert):\s*//' | \
        tr '[:upper:]' '[:lower:]' | \
        sed 's/[^a-z0-9]/-/g' | \
        sed 's/--*/-/g' | \
        sed 's/^-//' | \
        sed 's/-$//' | \
        cut -c1-50)

    branch_name="pr/${branch_name}"

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📝 Processing: $subject"
    echo "   Hash: $hash"
    echo "   Branch: $branch_name"
    echo ""

    if [[ "$DRY_RUN" == true ]]; then
        # Dry run - just show what would happen
        echo "   Would create PR: $subject"
        echo ""
        processed=$((processed + 1))
    else
        # Fetch latest main from remote
        git fetch origin main:main 2>/dev/null || true

        # Create new branch from main and check it out
        git checkout -b "$branch_name" main

        # Cherry-pick the commit with automatic conflict resolution favoring the new changes
        if git cherry-pick "$hash" 2>/dev/null; then
            # Cherry-pick succeeded
            echo "   ✓ Cherry-pick successful"
        else
            # Cherry-pick failed, try to resolve automatically
            echo "   ⚠ Conflict detected, attempting auto-resolution..."

            # Accept all changes from the cherry-picked commit
            git checkout --theirs . 2>/dev/null || true
            git add -A

            # Check if we have a clean state
            if git cherry-pick --continue 2>/dev/null; then
                echo "   ✓ Auto-resolved conflicts"
            else
                # Still failing, abort and skip this commit
                git cherry-pick --abort
                git checkout feature/agent-server
                git branch -D "$branch_name" 2>/dev/null || true
                echo "   ✗ Skipping due to unresolvable conflicts"
                echo ""
                skipped=$((skipped + 1))
                continue
            fi
        fi

        # Push the branch
        git push -u origin "$branch_name"

        # Get full commit message for PR body
        commit_msg=$(git log -1 --format="%b" "$hash")

        # Create PR using gh CLI
        # The title will be the commit subject, body will be the commit message body
        if [ -n "$commit_msg" ]; then
            gh pr create --base main --head "$branch_name" --title "$subject" --body "$commit_msg"
        else
            gh pr create --base main --head "$branch_name" --title "$subject" --body "$(git log -1 --format=%B $hash)"
        fi

        echo "✅ Created PR for: $subject"
        echo ""
        processed=$((processed + 1))
    fi
done

# Return to original branch (only if not in dry run)
if [[ "$DRY_RUN" == false ]]; then
    git checkout feature/agent-server
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
if [[ "$DRY_RUN" == true ]]; then
    echo "🔍 Dry run complete!"
    echo "   Would process: $processed commits"
    echo "   Would skip: $skipped merge commits"
    echo ""
    echo "To actually create the PRs, run:"
    echo "  bash create-prs.sh"
else
    echo "✨ All PRs created successfully!"
    echo "   Processed: $processed commits"
    echo "   Skipped: $skipped merge commits"
    echo ""
    echo "To view all PRs:"
    echo "  gh pr list"
fi
