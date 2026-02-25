# Test Credentials

Comprehensive test suite for the SpaceMolt credential management system.

## Overview

The `test-credentials` tool validates all credential providers and backends in the SpaceMolt system. It ensures that agents can authenticate using various storage mechanisms and that the credential system handles errors correctly.

## Features

### Phase 1 Tests: Provider Validation
- **StaticProvider**: In-memory credential storage
- **FileProvider**: JSON file-based credential storage
- **FallbackProvider**: Cascading provider with automatic fallback
- **LegacyProvider**: Legacy single-file credential format
- **EnvProvider**: Environment variable-based credentials
- **ListAgents Aggregation**: Agent discovery across multiple providers
- **Error Handling**: Proper error types for missing credentials
- **File Permissions**: Security validation (0600 permissions)

### Phase 2 Tests: SQLite Encryption
- **SQLiteProvider**: Encrypted SQLite database storage
- **Encryption/Decryption**: AES-256 encryption for passwords
- **ListAgents**: Agent discovery from SQLite
- **RemoveCredentials**: Secure credential deletion

## Usage

### Basic Usage

```bash
# Run all credential tests
go run ./cmd/test-credentials

# Or use the built binary
./test-credentials
```

### Example Output

```
=== Phase 1 Credential Provider Test ===

Testing in: /tmp/spacemolt-credentials-test-12345

1. Testing StaticProvider...
   ✓ StaticProvider: username=testuser, password=testpassword

2. Testing FileProvider...
   ✓ FileProvider: stored and retrieved username=explorer-7

3. Testing FallbackProvider...
   ✓ FallbackProvider: got credentials from file provider (username=explorer-7)

4. Testing LegacyProvider...
   ✓ LegacyProvider: agent-1 username=legacyuser, agent-2 username=legacyuser_agent-2

5. Testing EnvProvider...
   ✓ EnvProvider: loaded from environment (username=miner-2)
   ✓ EnvProvider.ListAgents: found 1 agent(s) in environment

6. Testing FallbackProvider ListAgents aggregation...
   ✓ ListAgents: found 3 agent(s) across providers

7. Testing error handling...
   ✓ Error handling: correct error for missing credentials

8. Testing file permissions...
   ✓ File permissions: 0600 (secure)

=== All Phase 1 Tests Passed ===

=== Phase 2: SQLite Encryption Test ===

9. Testing SQLiteProvider with encryption...
   ✓ SQLiteProvider: stored encrypted credentials
   ✓ SQLiteProvider: retrieved and decrypted credentials (username=agent-1-user)

10. Testing SQLiteProvider ListAgents...
   ✓ ListAgents: found 1 agent(s)

11. Testing SQLiteProvider RemoveCredentials...
   ✓ RemoveCredentials: successfully removed credentials

=== All Phase 2 Tests Passed ===

🎉 Multi-Agent Authentication System - Phase 1 & 2 Complete!
```

## Building

```bash
# Build the binary
go build -o bin/test-credentials ./cmd/test-credentials

# Run the built binary
./bin/test-credentials
```

## Test Details

### Phase 1: Provider Tests

#### 1. StaticProvider
Tests in-memory credential storage for scenarios where credentials are provided programmatically.

**Validates**:
- Credential retrieval
- Username/password matching
- Empire field handling

#### 2. FileProvider
Tests JSON file-based storage in `data/agents/{agent-id}/credentials.json`.

**Validates**:
- Credential storage to disk
- Credential retrieval from disk
- Proper JSON encoding/decoding
- Agent directory creation

#### 3. FallbackProvider
Tests cascading provider that tries multiple sources in order.

**Validates**:
- Sequential fallback through providers
- First successful provider is used
- All providers exhausted → error
- aggregation of ListAgents across providers

#### 4. LegacyProvider
Tests legacy single-file credential format for backward compatibility.

**Validates**:
- Legacy file format support
- Agent ID suffixing to username
- Multiple agents from single file

#### 5. EnvProvider
Tests environment variable-based credentials.

**Validates**:
- Reading from `SPACEMOLT_AGENT_{AGENT_ID}_USERNAME`
- Reading from `SPACEMOLT_AGENT_{AGENT_ID}_TOKEN`
- Agent listing from environment
- Proper error handling for missing vars

#### 6. ListAgents Aggregation
Tests agent discovery across multiple providers.

**Validates**:
- Combines agents from all providers
- Removes duplicates
- Returns sorted list

#### 7. Error Handling
Tests proper error types for missing credentials.

**Validates**:
- `ErrCredentialsNotFound` for missing agents
- Error type checking with `IsErrCredentialsNotFound()`
- Proper error wrapping

#### 8. File Permissions
Tests security validation for credential files.

**Validates**:
- Files have 0600 permissions (owner read/write only)
- Validation catches insecure permissions
- Security best practices

### Phase 2: SQLite Encryption Tests

#### 9. SQLiteProvider with Encryption
Tests encrypted SQLite database storage.

**Validates**:
- AES-256 encryption for passwords
- Database initialization
- Credential storage (encrypted)
- Credential retrieval (decrypted)
- Password integrity verification

#### 10. ListAgents
Tests agent discovery from SQLite database.

**Validates**:
- Returns all registered agents
- Proper SQL query execution
- Error handling for empty database

#### 11. RemoveCredentials
Tests secure credential deletion.

**Validates**:
- Credential removal from database
- Subsequent retrieval fails
- Proper error types after removal

## Temporary Directory

All tests run in a temporary directory that is automatically cleaned up:

```
/tmp/spacemolt-credentials-test-{random}
```

This ensures tests don't interfere with:
- Production credentials in `data/agents/`
- User's home directory credentials in `~/.spacemolt/`
- Any existing SQLite databases

## Mock Provider

The test suite includes a `mockProvider` for testing fallback behavior:

```go
type mockProvider struct {
    exists  bool           // Whether credentials exist
    agents  []string       // List of agents to return
    getFunc func(...)      // Custom GetCredentials function
}
```

## Dependencies

The test tool uses the `pkg/credentials` package which provides:

- `StaticProvider`
- `FileProvider`
- `FallbackProvider`
- `LegacyProvider`
- `EnvProvider`
- `SQLiteProvider`
- `Encryptor` (AES-256)
- Error types and validation

## Exit Codes

- `0`: All tests passed
- `1`: One or more tests failed

## Integration with CI/CD

This tool is ideal for CI/CD pipelines:

```yaml
- name: Test credential system
  run: |
    go run ./cmd/test-credentials
```

## Related Documentation

- [pkg/credentials](../../pkg/credentials/) - Credential system implementation
- [agent-server](../agent-server/README.md) - Multi-agent credential usage
- [register-agent](../register-agent/README.md) - Agent registration

## License

Part of the SpaceMolt project.
