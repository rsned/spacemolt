# Spacemolt - Codebase Context for Claude

When adding any sleep or pauses, always use one of the predefined Sleep constants in `pkg/game/constants.go`. If none are appropriate, prompt the user to add another value.

## Build & Test

After implementing changes, always run `go build ./...` and `go test ./...` before committing. For frontend changes, run the appropriate build command to verify no compilation errors.

## Project Context

Always check actual API/server response struct field names before coding against them — do NOT assume field names.

To add a new game command or strategy, use the `add-game-command` skill.

## Git Workflow

When asked to commit and push, use `git add -A && git commit -m '<message>' && git push` without excessive verification. Do not spend multiple tool calls checking if the push succeeded — trust the exit code.

Check .gitignore before committing new files in directories that may have broad ignore patterns (e.g., `docs/*.md`). If a file is being ignored unexpectedly, add a negation rule like `!docs/USAGE.md`.

## Conventions

- **State access**: `State` is the central game state struct. Thread-safe access via `state.Clone()`. Updated by `handleResponse()` on every server message.
- **Freshness thresholds**: Resource POIs: 6 hours, Default POIs: 1 week, Stations: 1 day, Systems: 1 day.
- **Compiled binaries**: Any compiled or built binary must go in bin/, not in the top project directory.
