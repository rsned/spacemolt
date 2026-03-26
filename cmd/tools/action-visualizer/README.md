# Action Space Visualizer

Interactive web tool for exploring the game's decision space. Shows which actions are available in different game states and how aggressively the action space can be pruned.

## Usage

```bash
# Using hardcoded action registry (default):
go run ./cmd/tools/action-visualizer/

# Using OpenAPI spec (auto-discovers new commands):
go run ./cmd/tools/action-visualizer/ -spec server_docs/openapi.json
```

Opens a browser at `http://localhost:8077`.

With `-spec`, the tool parses all commands from the OpenAPI spec, merges them with annotated preconditions from `pkg/actionspace/annotations.go`, and warns about any new commands that need precondition annotations.

## Layout

- **Top bar** — Branching factor, valid/total actions, pruned %, top pruning reason
- **Left panel** — Toggleable game state controls (docked/undocked, combat, resources, location, etc.)
- **Main area** — D3 radial dendrogram: center = current state, middle ring = 15 action categories, outer ring = individual actions

Valid actions are colored by category. Pruned actions are greyed out. Hover any action leaf for details including failed preconditions.

## Architecture

The Go server embeds all static assets via `go:embed` and exposes a single API endpoint:

- `POST /api/evaluate` — accepts a `GameContext` JSON body, returns the evaluated `ActionSpace` (tree + stats)

The browser builds a `GameContext` from the toggle/slider state, POSTs it on every change, and re-renders the D3 tree from the response.

## Dependencies

- `pkg/actionspace` — action registry, preconditions, evaluation engine
- D3.js v7 (vendored in `static/`)
