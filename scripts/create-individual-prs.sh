#!/bin/bash
# Script to create individual PRs for each commit on feature/agent-server

set -e

# Get list of commit hashes (oldest first, no merges)
commits=$(git log main..feature/agent-server --no-merges --format="%H" --reverse)

# Counter for tracking progress
count=0
total=$(echo "$commits" | wc -l)

echo "Found $total commits to create PRs for"
echo ""

for commit in $commits; do
  count=$((count + 1))

  # Get commit subject and body
  subject=$(git log -1 --format="%s" "$commit")
  body=$(git log -1 --format="%b" "$commit")

  # Create branch name from commit subject
  # Remove "feat: ", "fix: ", etc. prefixes and convert to kebab-case
  branch_name=$(echo "$subject" | \
    sed 's/^feat: //i; s/^fix: //i; s/^refactor: //i' | \
    tr '[:upper:]' '[:lower:]' | \
    sed 's/[^a-z0-9]\+/-/g; s/^-//; s/-$//' | \
    cut -c1-50)

  # Add prefix to avoid conflicts
  branch_name="pr/${branch_name}"

  echo "[$count/$total] Processing: $subject"
  echo "  Branch: $branch_name"
  echo "  Commit: $commit"

  # Check if branch already exists
  if git show-ref --verify --quiet "refs/heads/$branch_name"; then
    echo "  ⚠️  Branch $branch_name already exists, skipping..."
    echo ""
    continue
  fi

  # Create new branch from main without checking it out
  git branch "$branch_name" main
  git checkout "$branch_name" -q

  # Cherry-pick the commit
  if ! git cherry-pick "$commit" 2>/dev/null; then
    echo "  ❌ Cherry-pick failed for $commit"
    git cherry-pick --abort
    git checkout feature/agent-server -q
    git branch -D "$branch_name"
    echo ""
    continue
  fi

  # Push the branch (force in case old branch exists on remote)
  git push -u origin "$branch_name" --force-with-lease -q

  # Create PR using gh CLI
  # Combine subject and body for PR description
  if [ -n "$body" ]; then
    pr_body="$body"
  else
    pr_body="$subject"
  fi

  # Create PR
  gh pr create \
    --title "$subject" \
    --body "$pr_body" \
    --base main \
    --head "$branch_name" 2>/dev/null || {
      echo "  ❌ Failed to create PR"
      git checkout feature/agent-server -q
      echo ""
      continue
    }

  echo "  ✅ PR created successfully"
  echo ""
done

# Return to original branch
git checkout feature/agent-server -q

echo "Done! Created PRs for $count commits"
echo "You can view all PRs with: gh pr list"
