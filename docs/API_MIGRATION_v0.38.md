# API Migration: Token → Password (v0.38.0)

## Overview

SpaceMolt API v0.38.0 renamed the authentication field from `token` to `password` for semantic clarity. This migration updates the entire codebase to use the new terminology while maintaining backward compatibility.

## What Changed

### API Changes (v0.38.0)
- **Registration**: Server returns `password` field instead of `token`
- **Login**: Accepts `password` parameter instead of `token`
- **Semantic meaning**: Password is permanent (64-char hex) with no recovery

### Code Changes

#### Core Data Structures
- `pkg/credentials/provider.go`: `Credentials.Token` → `Credentials.Password`
- `pkg/game/types.go`: `State.Token` → `State.Password`

#### Game Client
- `pkg/game/client.go`:
  - Client struct field: `token` → `password`
  - `NewClient()` parameter: `token` → `password`
  - Login payload: `"token"` → `"password"`
  - Registration response: supports both `"password"` and `"token"` (legacy)

#### Credentials Providers (with auto-migration)
- `pkg/credentials/file.go`: Reads both `token` and `password`, writes `password`
- `pkg/credentials/legacy.go`: Reads both fields, migrates automatically
- `pkg/credentials/sqlite.go`: Uses `password` (column still named `token` for DB compatibility)
- `pkg/credentials/env.go`: Checks `PASSWORD` then `TOKEN` env vars

#### Application Code
- `pkg/agent/manager.go`: Uses `creds.Password` and `state.Password`
- `cmd/watcher/main.go`: All `token` variables → `password`
- `cmd/register-agent/main.go`: All `token` references → `password`

#### Tests
- All test files updated to use `Password` instead of `Token`

## Backward Compatibility

### ✅ Old credential files work automatically
- File provider reads both `"token"` and `"password"` fields
- Auto-migrates on load: copies `token` to `password` and resaves
- No manual intervention required

### ✅ Environment variables
- New: `SPACEMOLT_AGENT_<ID>_PASSWORD`
- Legacy: `SPACEMOLT_AGENT_<ID>_TOKEN` (still supported)
- Password takes precedence if both set

### ✅ API responses
- Client accepts both `"password"` and `"token"` in registration response
- Prioritizes `"password"`, falls back to `"token"`

## Migration Guide

### For Users

**No action required!** Old credential files will automatically migrate when loaded.

### For Developers

If you have custom code:

```go
// OLD
creds := &credentials.Credentials{
    Username: "agent",
    Token:    "abc123",
}

// NEW
creds := &credentials.Credentials{
    Username: "agent",
    Password: "abc123",
}
```

### Environment Variables

```bash
# NEW (recommended)
export SPACEMOLT_AGENT_EXPLORER_7_PASSWORD="your-password"

# OLD (still works)
export SPACEMOLT_AGENT_EXPLORER_7_TOKEN="your-password"
```

## Files Modified

### Core (6 files)
- pkg/credentials/provider.go
- pkg/credentials/file.go
- pkg/credentials/legacy.go
- pkg/credentials/sqlite.go
- pkg/credentials/env.go
- pkg/game/types.go

### Game Client (1 file)
- pkg/game/client.go

### Application (2 files)
- pkg/agent/manager.go
- cmd/watcher/main.go

### CLI Tools (1 file)
- cmd/register-agent/main.go

### Tests (4 files)
- pkg/game/client_test.go
- pkg/game/client_integration_test.go
- pkg/credentials/provider_test.go
- pkg/agent/manager_enhanced_test.go

## Testing

### Unit Tests
```bash
go test ./pkg/credentials/...
go test ./pkg/game/...
go test ./pkg/agent/...
```

### Integration Test
```bash
# Register new agent (receives password from API)
./register-agent --username test-agent

# Verify old credentials still work
# (they auto-migrate on first load)
```

## Breaking Changes

**None!** All changes are backward compatible.

## Future Work

- SQLite database migration to rename `token` column to `password` (cosmetic)
- Deprecation warning for `TOKEN` environment variable (v1.0)
- Remove legacy support in v2.0
