#!/usr/bin/env bash
# Generate SVG graphs from all skill .dot files
# Usage: ./scripts/generate-skill-graphs.sh [skills-dir]

set -euo pipefail

SKILLS_DIR="${1:-data/skills}"
SKILL_GRAPH="cmd/tools/skill-graph/main.go"

if ! command -v dot &>/dev/null; then
  echo "error: graphviz 'dot' command not found — install with: sudo apt install graphviz" >&2
  exit 1
fi

# First regenerate .dot files from YAML sources
count=0
for yaml in "$SKILLS_DIR"/*.yaml "$SKILLS_DIR"/*.yml; do
  [ -f "$yaml" ] || continue
  base="${yaml%.*}"
  dot_file="${base}.dot"
  svg_file="${base}.svg"

  echo "generating: $dot_file"
  go run "$SKILL_GRAPH" -skills-dir "$SKILLS_DIR" -o "$dot_file" "$yaml"

  echo "converting: $svg_file"
  dot -Tsvg "$dot_file" -o "$svg_file"
  count=$((count + 1))
done

echo "done: $count skill graphs generated"
