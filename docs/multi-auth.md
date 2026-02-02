# Multi-Agent Authentication Design

## Problem Statement

Currently, the Spacemolt multi-agent system uses a single set of credentials (`.spacemolt-credentials.json`) for all agents. This creates several limitations:

1. **No Agent Identity**: All agents appear as the same player (e.g., `Watcher_12345` + agent ID)
2. **Shared Session**: All agents share one player's ship, cargo, and location
3. **Authentication Conflicts**: Multiple agents using the same credentials can cause race conditions
4. **No True Autonomy**: Agents cannot act independently as separate players

Each agent needs its own authentication credentials to establish independent WebSocket connections and act as autonomous players in the game world.

## Design Goals

1. **Security**: Credentials must be stored safely with appropriate access controls
2. **Scalability**: Support many agents without management overhead
3. **Flexibility**: Support multiple credential sources and formats
4. **Ease of Use**: Simple setup and management for users
5. **Backward Compatibility**: Existing single-credential setups continue to work
6. **Isolation**: Each agent's credentials are completely isolated
7. **Persistence**: Credentials survive restarts

## Proposed Solution: Hybrid Credential Management

A flexible, layered approach supporting multiple credential sources with a unified interface.

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Agent Manager                             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐   │
│  │Agent 1   │ │Agent 2   │ │Agent 3   │ │   Agent N    │   │
│  │explorer-7│ │miner-2   │ │fighter-1 │ │  trader-1    │   │
│  └────┬─────┘ └────┬─────┘ └────┬─────┘ └──────┬───────┘   │
└───────┼────────────┼────────────┼──────────────┼───────────┘
        │            │            │              │
        └────────────┼────────────┼──────────────┘
                     │            │
              ┌──────▼────────────▼──────┐
              │   Credential Provider    │
              │   (Unified Interface)    │
              └──────┬───────────────────┘
                     │
        ┌────────────┼────────────┐
        │            │            │
   ┌────▼────┐ ┌────▼────┐ ┌────▼────┐
   │  File   │ │  SQLite │ │Keychain │
   │Provider │ │Provider │ │Provider │
   └─────────┘ └─────────┘ └─────────┘
```

## Credential Sources (Priority Order)

### 1. Agent-Specific Credential Files (Primary)

**Location**: `data/agents/<agent-id>/credentials.json`

**Format**:
```json
{
  "username": "agent-username",
  "token": "agent-auth-token",
  "empire": "voidborn"
}
```

**Advantages**:
- Follows existing agent directory structure
- Perfect isolation between agents
- Easy to version control (if using placeholder tokens)
- Simple to backup and manage
- Works well with development workflows

**Use Cases**:
- Development environments
- Testing multiple agent configurations
- CI/CD pipelines with credential injection

### 2. SQLite Knowledge Base (Secondary)

**Table**: `agent_credentials`

**Schema**:
```sql
CREATE TABLE agent_credentials (
    agent_id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    token TEXT NOT NULL,
    empire TEXT DEFAULT 'voidborn',
    created_at TEXT NOT NULL,
    last_used TEXT,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
```

**Encryption**: Tokens should be encrypted at rest using AES-256-GCM with a key derived from:
- System keyring (preferred), or
- Passphrase file (`~/.spacemolt/key`), or
- Environment variable (`SPACEMOLT_ENCRYPTION_KEY`)

**Advantages**:
- Centralized credential storage
- Already using SQLite for persistence
- Can track metadata (created, last used)
- Encryption support built-in

**Use Cases**:
- Production deployments
- Large agent fleets
- Credential lifecycle management

### 3. System Keyring (Tertiary)

**Platforms**:
- Linux: gnome-keyring, kwallet
- macOS: Keychain
- Windows: Credential Manager

**Advantages**:
- Most secure option
- OS-managed encryption
- User already familiar with keyring

**Disadvantages**:
- Complex cross-platform implementation
- Additional dependency
- May not work in all environments (containers, headless servers)

### 4. Environment Variables (Fallback)

**Format**:
```bash
SPACEMOLT_AGENT_EXPLORER_7_USERNAME="explorer-7"
SPACEMOLT_AGENT_EXPLORER_7_TOKEN="token-here"
SPACEMOLT_AGENT_MINER_2_USERNAME="miner-2"
SPACEMOLT_AGENT_MINER_2_TOKEN="token-here"
```

**Naming Convention**: `SPACEMOLT_AGENT_<AGENT_ID>_<FIELD>`

**Advantages**:
- Standard for containerized deployments
- No file I/O required
- Works well with secrets management systems (HashiCorp Vault, AWS Secrets Manager)

**Disadvantages**:
- Not persistent across shell sessions
- Can clutter environment
- Security concerns (visible in process list)

### 5. Legacy Single Credential (Backward Compatibility)

**Location**: `.spacemolt-credentials.json`

**Behavior**: If no agent-specific credentials found, fall back to single credential for all agents

**Use Case**: Maintaining backward compatibility with existing setups

## Unified Credential Provider Interface

### Core Interface

```go
package credentials

import "context"

// Credentials represents authentication credentials
type Credentials struct {
    Username string
    Token     string
    Empire    string // Faction/empire for registration
}

// Provider provides credentials for agents
type Provider interface {
    // GetCredentials retrieves credentials for an agent
    GetCredentials(ctx context.Context, agentID string) (*Credentials, error)

    // StoreCredentials saves credentials for an agent
    StoreCredentials(ctx context.Context, agentID string, creds *Credentials) error

    // RemoveCredentials deletes credentials for an agent
    RemoveCredentials(ctx context.Context, agentID string) error

    // ListAgents returns all agent IDs with stored credentials
    ListAgents(ctx context.Context) ([]string, error)
}
```

### Provider Implementations

```go
// FileProvider loads credentials from agent-specific files
type FileProvider struct {
    agentsDir string // e.g., "data/agents"
}

// SQLiteProvider loads credentials from SQLite database
type SQLiteProvider struct {
    db        *sql.DB
    encryptor *Encryptor
}

// KeyringProvider loads credentials from system keyring
type KeyringProvider struct {
    service string // "spacemolt-agent-credentials"
}

// EnvProvider loads credentials from environment variables
type EnvProvider struct {
    prefix string // "SPACEMOLT_AGENT_"
}

// FallbackProvider tries multiple providers in order
type FallbackProvider struct {
    providers []Provider
}

// LegacyProvider provides single credential for all agents
type LegacyProvider struct {
    credsFile string
}
```

## Implementation Plan

### Phase 1: Core Infrastructure

**Tasks**:
1. Create `pkg/credentials/` package
2. Define `Provider` interface and `Credentials` struct
3. Implement `FileProvider` for agent-specific files
4. Implement `LegacyProvider` for backward compatibility
5. Implement `FallbackProvider` to chain providers

**Files**:
- `pkg/credentials/provider.go` - Interface and types
- `pkg/credentials/file.go` - File-based provider
- `pkg/credentials/legacy.go` - Legacy single credential provider
- `pkg/credentials/fallback.go` - Multi-provider fallback logic

**Testing**:
- Unit tests for each provider
- Integration test with mock credentials
- Verify backward compatibility with legacy setup

### Phase 2: SQLite Provider

**Tasks**:
1. Add `agent_credentials` table to SQLite schema
2. Implement `SQLiteProvider` with encryption
3. Add migration to create table
4. Implement key derivation logic
5. Add credential management CLI commands

**Files**:
- `pkg/knowledge/sqlite_migrations.go` - Add migration for credentials table
- `pkg/credentials/sqlite.go` - SQLite provider implementation
- `pkg/credentials/crypto.go` - Encryption utilities
- `cmd/watcher/main.go` - Add credential management commands

**Encryption Details**:
```go
type Encryptor struct {
    key []byte // 32-byte AES-256 key
}

// Key sources (tried in order):
func (e *Encryptor) LoadKey() error {
    // 1. System keyring
    // 2. ~/.spacemolt/key file (0600 permissions)
    // 3. SPACEMOLT_ENCRYPTION_KEY environment variable
    // 4. Generate new key and store in keyring
}

// Encrypt/Decrypt using AES-256-GCM
func (e *Encryptor) Encrypt(plaintext string) (string, error)
func (e *Encryptor) Decrypt(ciphertext string) (string, error)
```

**Schema Migration**:
```sql
-- Migration version 2
CREATE TABLE IF NOT EXISTS agent_credentials (
    agent_id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    token_encrypted TEXT NOT NULL,
    empire TEXT DEFAULT 'voidborn',
    created_at TEXT NOT NULL,
    last_used TEXT,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
```

**CLI Commands**:
```bash
# List stored credentials
./watcher --credentials-list

# Add/update credentials
./watcher --credentials-set explorer-7 --username "explorer-7" --token "xxx"

# Remove credentials
./watcher --credentials-remove explorer-7

# Export credentials (for backup)
./watcher --credentials-export backup.json

# Import credentials
./watcher --credentials-import backup.json
```

### Phase 3: Integration with Agent Manager

**Tasks**:
1. Update `AgentManager` to use `Provider` interface
2. Pass credentials when creating game clients
3. Handle credential loading errors gracefully
4. Add credential validation on startup

**Files Modified**:
- `pkg/agent/manager.go` - Accept Provider in constructor
- `pkg/agent/base.go` - Pass credentials to game client
- `cmd/watcher/main.go` - Initialize and configure providers

**Example Changes**:
```go
// manager.go
type Manager struct {
    agents        map[string]Agent
    kb            knowledge.Base
    llm           *llm.Client
    credsProvider credentials.Provider // NEW
    mu            sync.RWMutex
    maxAgents     int
}

func NewManager(
    kb knowledge.Base,
    llmClient *llm.Client,
    credsProvider credentials.Provider, // NEW
    maxAgents int,
) *Manager {
    return &Manager{
        kb:            kb,
        llm:           llmClient,
        credsProvider: credsProvider,
        maxAgents:     maxAgents,
    }
}

func (m *Manager) SpawnAgent(ctx context.Context, personality Personality) (Agent, error) {
    // ... existing validation ...

    // Load credentials for this agent
    creds, err := m.credsProvider.GetCredentials(ctx, personality.ID)
    if err != nil {
        return nil, fmt.Errorf("failed to get credentials for %s: %w", personality.ID, err)
    }

    // Create game client with agent's credentials
    client := game.NewClient(
        "wss://game.spacemolt.com/ws",
        creds.Username,
        creds.Token,
        debugLogger,
    )

    // ... rest of spawn logic ...
}
```

### Phase 4: Enhanced Providers

**Tasks**:
1. Implement `EnvProvider` for environment variables
2. Implement `KeyringProvider` for system keyring
3. Add credential validation (check if token works)
4. Add credential refresh/rotation support

**Files**:
- `pkg/credentials/env.go` - Environment variable provider
- `pkg/credentials/keyring.go` - System keyring provider
- `pkg/credentials/validator.go` - Credential validation logic

### Phase 5: Credential Management Tools

**Tasks**:
1. Create standalone credential management tool
2. Add interactive credential setup wizard
3. Implement credential bulk import/export
4. Add credential health check command

**Files**:
- `cmd/spacemolt-creds/main.go` - Credential management CLI
- `pkg/credentials/wizard.go` - Interactive setup

**CLI Usage**:
```bash
# Interactive setup
spacemolt-creds setup

# Add agent credentials
spacemolt-creds add explorer-7
# Prompts for username and token securely

# Validate credentials
spacemolt-creds validate explorer-7

# Rotate credentials
spacemolt-creds rotate explorer-7

# Migrate from legacy to new format
spacemolt-creds migrate-from-legacy
```

## Security Considerations

### File Permissions

All credential files should have restrictive permissions:
- `data/agents/<agent-id>/credentials.json`: `0600` (owner read/write only)
- `~/.spacemolt/key`: `0600`
- SQLite database: `0600`

### Encryption at Rest

- SQLite tokens must be encrypted
- Use AES-256-GCM (authenticated encryption)
- Key derivation: PBKDF2, scrypt, or Argon2 with high iteration count
- Never log tokens or encryption keys

### Transit Security

- Tokens transmitted over WSS (WebSocket Secure)
- HTTPS for any credential management APIs
- Consider token rotation policies

### Access Control

```go
// Validate file permissions on startup
func ValidateFilePermissions(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }

    mode := info.Mode()
    perm := mode.Perm()

    // Only owner should have read/write (0600)
    if perm != 0600 {
        return fmt.Errorf("insecure permissions: %v (expected 0600)", perm)
    }

    return nil
}
```

## Migration Path

### For Existing Users

1. **Automatic Migration**: On first run with new code:
   - Detect `.spacemolt-credentials.json` exists
   - Log warning about multi-agent setup
   - Use `LegacyProvider` for backward compatibility
   - Prompt user to run migration command

2. **Migration Command**:
   ```bash
   # Convert legacy credentials to per-agent files
   spacemolt-creds migrate --from=legacy --to=file

   # Convert legacy credentials to SQLite
   spacemolt-creds migrate --from=legacy --to=sqlite
   ```

3. **Credential Generation**: For users who need multiple accounts:
   - Provide helper to generate accounts via game API
   - Or document manual account creation process

### For New Users

1. **Setup Wizard**: `spacemolt-creds setup`
   - Ask: "How many agents will you run?"
   - Prompt for each agent's credentials
   - Save to preferred provider (file or SQLite)

2. **Documentation**: Update README with:
   - Credential setup instructions
   - Multi-account creation guide
   - Security best practices

## Configuration

### CLI Flags

```bash
./watcher \
  --credentials-provider=file \  # file, sqlite, keyring, env, auto
  --credentials-file=*.json \     # glob pattern for file provider
  --sqlite-encryption-keyfile=~/.spacemolt/key
```

### Config File

**Location**: `~/.spacemolt/config.yaml`

```yaml
credentials:
  # Provider order: first found wins
  providers:
    - type: file
      agents_dir: ./data/agents
    - type: sqlite
      database: ./spacemolt-knowledge.db
      encryption:
        key_file: ~/.spacemolt/key
        # or use keyring
        use_keyring: true
    - type: legacy
      file: .spacemolt-credentials.json

  # Validation: check credentials on startup
  validate_on_startup: true

  # Auto-refresh tokens (if supported by game)
  auto_refresh: true
  refresh_threshold: 24h
```

## Error Handling

### Credential Not Found

```go
creds, err := provider.GetCredentials(ctx, agentID)
if err != nil {
    if errors.Is(err, ErrCredentialsNotFound) {
        // Helpful error message
        return nil, fmt.Errorf(
            "no credentials found for agent %s\n"+
            "Options:\n"+
            "  1. Create %s\n"+
            "  2. Run: spacemolt-creds add %s\n"+
            "  3. Use --credentials-provider=legacy for single-credential mode",
            agentID, credFilePath(agentID), agentID,
        )
    }
    return nil, err
}
```

### Invalid Credentials

```go
// After connection attempt
if resp.Type == "error" && resp.Payload["error"] == "authentication_failed" {
    provider.MarkInvalid(ctx, agentID)
    log.Printf("Agent %s: Invalid credentials, please refresh", agentID)
}
```

## Testing Strategy

### Unit Tests

- Each provider implementation
- Encryption/decryption
- Error handling
- Fallback logic

### Integration Tests

- Load credentials from source
- Pass to game client
- Verify successful connection
- Test credential rotation

### Security Tests

- Verify file permissions
- Test encryption/decryption
- Validate key derivation
- Check for token leakage in logs

### Mock Server Tests

- Run against local test server
- Test multiple concurrent agents
- Verify isolation (each agent has unique session)

## Performance Considerations

- **Lazy Loading**: Load credentials only when spawning agent
- **Caching**: SQLite provider can cache decrypted credentials in memory (with TTL)
- **Connection Pooling**: Reuse database connections for credential lookups
- **Async Validation**: Validate credentials in background after spawn

## Future Enhancements

1. **OAuth2 Support**: If game adds OAuth, support token refresh flows
2. **Credential Sync**: Sync credentials across multiple machines
3. **Shared Credentials**: Allow multiple agents to share credentials (with coordination)
4. **Credential Expiration**: Track token expiration and auto-refresh
5. **Audit Logging**: Log credential access and usage
6. **Hardware Security Modules**: Support HSMs for enterprise deployments

## Rollout Plan

### Week 1: Infrastructure
- Phase 1 implementation
- Unit tests
- Documentation

### Week 2: SQLite & Integration
- Phase 2 implementation
- Phase 3 integration
- Integration tests

### Week 3: Enhanced Features
- Phase 4 implementation
- Phase 5 tools
- Migration utilities

### Week 4: Testing & Polish
- End-to-end testing
- Security audit
- Documentation updates
- Beta testing with selected users

## Success Criteria

1. ✅ Each agent can have unique credentials
2. ✅ Credentials stored securely with encryption
3. ✅ Backward compatibility maintained
4. ✅ Simple setup for new users
5. ✅ CLI tools for credential management
6. ✅ Comprehensive documentation
7. ✅ All tests passing
8. ✅ Security audit passed
