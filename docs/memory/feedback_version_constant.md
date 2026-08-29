---
name: Update BuiltForAPIVersion when updating client
description: When updating response structs or command signatures for a new server API version, always bump BuiltForAPIVersion in pkg/version/checker.go
type: feedback
---

When updating the client to match a new server API version (response structs, command signatures, new commands), always update the `BuiltForAPIVersion` constant in `pkg/version/checker.go` to match.

**Why:** The constant records what server version the code was built against. If it's not updated, the version check will show false warnings even when the client is current. Previously, the version was read from `server_docs/api.md` at runtime, but that file changes independently when docs are downloaded.

**How to apply:** Any time you modify `pkg/game/serverapi/responses.go`, `pkg/game/client.go`, or `pkg/game/interface.go` to match a new server API, also update the constant:
```go
// pkg/version/checker.go
const BuiltForAPIVersion = "v0.XXX.X"
```
