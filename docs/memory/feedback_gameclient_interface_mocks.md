---
name: feedback_gameclient_interface_mocks
description: Adding methods to the GameClient interface breaks explicit mocks; go build does not catch it
metadata: 
  node_type: memory
  type: feedback
  originSessionId: ca64e2a6-eb83-4db2-b98e-60306d83990c
---

When you add a method to the `GameClient` interface (`pkg/game/interface.go`),
**three** types must gain the method — all implement it explicitly (no embedded
interface), so a new method makes each stop satisfying `GameClient`:

1. **`MCPGameClient`** — the real second implementation. Method bodies live in
   **`pkg/game/mcp_game_client_commands.go`** (call `m.callTool(...)` then
   `m.updateStateFromResult` for mutations / `m.cacheResultAs` for queries). This
   one IS caught by `go build` via `var _ GameClient = (*MCPGameClient)(nil)`.
2. **`mockGameClient` in `pkg/agent/runner_test.go`** — no-op stub.
3. **`mockGameClient` in `pkg/skills/client_dispatcher_test.go`** — no-op stub.

**Why:** `go build ./...` compiles `MCPGameClient` (#1) but does NOT compile
`_test.go` files, so it passes even when the two test mocks (#2/#3) are broken.
The per-package gate `go test ./pkg/game/` also misses them because the broken
mocks live in *other* packages. Only `go test ./...` (or `go vet ./...`) surfaces
the failure (`*mockGameClient does not implement game.GameClient (missing method
X)`). Note: the editor/LSP diagnostics flag all three immediately.

**How to apply:** After any `GameClient` interface change, run the full
`go test ./...` (not just the touched package), or `go test ./... -run xxNONExx`
to compile-check every test package quickly. The two mocks use no-op stubs:
`func (m *mockGameClient) X(ctx context.Context, ...) error { return nil }`.
Relates to [[project_request_id_rollout]] and the phase-1 work in
[[project_api_currentness_round]].
