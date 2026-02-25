# SpaceMolt Test Agent Manager

> Integration testing tool for validating agent manager functionality with credential providers and multi-agent spawning.

## Overview

The test-agent-manager tool is a comprehensive integration test suite for the agent manager system. It validates credential provider integration, agent spawning, credential retrieval, and error handling. Essential for testing agent manager infrastructure before deployment.

## Features

### Core Functionality
- **🧪 Integration Testing** - Full agent manager integration tests
- **🔐 Credential Testing** - Test credential storage and retrieval
- **👥 Multi-Agent** - Test spawning multiple agents
- **💾 In-Memory Storage** - Uses temp directories for isolated testing
- **✅ Validation** - Verifies all components work together

### Test Coverage

1. **Knowledge Base Creation** - Test in-memory knowledge base
2. **Credential Provider** - Test file-based credential provider
3. **Agent Spawning** - Test spawning multiple agents
4. **Credential Retrieval** - Test GetCredentials functionality
5. **Error Handling** - Test missing credentials and spawn failures
6. **Cleanup** - Test agent shutdown and cleanup

## Quick Start

### Basic Usage

```bash
# Run all integration tests
go run ./cmd/test-agent-manager

# Build and run
go build -o bin/test-agent-manager ./cmd/test-agent-manager
./bin/test-agent-manager
```

## Test Flow

The tool runs a series of integration tests:

```
1. Create Knowledge Base
   ↓
2. Create Credential Provider
   ↓
3. Store Credentials for Multiple Agents
   ↓
4. Create LLM Client (Optional)
   ↓
5. Create Agent Manager
   ↓
6. Spawn Agents with Validation
   ↓
7. Verify Credentials Can Be Retrieved
   ↓
8. Test Credential Not Found Error
   ↓
9. Test Spawn Failure Without Credentials
   ↓
10. Cleanup (Stop All Agents)
```

## Examples

### Example 1: Successful Test Run

```bash
go run ./cmd/test-agent-manager
```

**Output:**
```
=== Phase 3: Agent Manager Integration Test ===

Testing in: /tmp/spacemolt-agent-manager-test-*

1. Creating knowledge base...
   ✓ Created in-memory knowledge base

2. Creating file credential provider...
   ✓ Stored credentials for explorer-7
   ✓ Stored credentials for miner-2
   ✓ Stored credentials for trader-1

3. Creating LLM client...
   Warning: Failed to initialize LLM client: dial tcp 127.0.0.1:11434: connect: connection refused
   ✓ Created LLM client

4. Creating agent manager...
   ✓ Created agent manager with credential provider

5. Spawning agents with credential validation...
   ✓ Spawned agent: Explorer 7 (ID: explorer-7)
   ✓ Verified credentials for explorer-7: explorer-7-user
   ✓ Spawned agent: Miner 2 (ID: miner-2)
   ✓ Verified credentials for miner-2: miner-2-user
   ✓ Spawned agent: Trader 1 (ID: trader-1)
   ✓ Verified credentials for trader-1: trader-1-user

6. Testing credential not found error...
   ✓ Correctly returns ErrCredentialsNotFound for missing credentials

7. Testing spawn failure without credentials...
   ✓ Correctly failed to spawn: credentials not found for agent: no-creds-agent

8. Cleaning up...
   ✓ Stopped all agents

=== All Phase 3 Tests Passed ===

🎉 Agent Manager Integration Complete!

Key features verified:
  • Manager accepts credential provider
  • SpawnAgent validates credentials exist
  • GetCredentials retrieves agent-specific credentials
  • Spawning fails without credentials
  • Multiple agents can have different credentials
```

### Example 2: Test with Ollama Running

```bash
# Start Ollama first
ollama serve

# Run tests
go run ./cmd/test-agent-manager
```

**Output:**
```
=== Phase 3: Agent Manager Integration Test ===

Testing in: /tmp/spacemolt-agent-manager-test-*

1. Creating knowledge base...
   ✓ Created in-memory knowledge base

2. Creating file credential provider...
   ✓ Stored credentials for explorer-7
   ✓ Stored credentials for miner-2
   ✓ Stored credentials for trader-1

3. Creating LLM client...
   ✓ Created LLM client
   ✓ Connected to Ollama successfully

4. Creating agent manager...
   ✓ Created agent manager with credential provider

... (rest of tests)
```

## Test Details

### Test 1: Knowledge Base Creation

Creates an in-memory knowledge base for testing.

**Verifies:**
- Knowledge base can be created
- No file system dependencies
- Clean state for each test run

### Test 2: Credential Provider

Creates a file-based credential provider in a temp directory.

**Verifies:**
- Provider can be initialized
- Temp directory can be created
- Credentials can be stored

**Stores:**
- explorer-7: explorer-7-user / password-abc-123
- miner-2: miner-2-user / password-def-456
- trader-1: trader-1-user / password-ghi-789

### Test 3: LLM Client

Creates an LLM client (optional).

**Verifies:**
- LLM client can be created
- Ollama connection works (if available)
- Graceful degradation if Ollama unavailable

### Test 4: Agent Manager Creation

Creates an agent manager with credential provider.

**Verifies:**
- Manager accepts credential provider
- Manager initializes correctly
- No errors during creation

### Test 5: Spawn Agents

Spawns three agents with stored credentials.

**Verifies:**
- Agents can be spawned
- SpawnAgent validates credentials exist
- Correct credentials are used for each agent
- Agent names and IDs match expectations

### Test 6: Credential Retrieval

Tests GetCredentials for spawned agents.

**Verifies:**
- GetCredentials retrieves correct credentials
- Username and password match stored values
- Each agent has unique credentials

### Test 7: Credential Not Found

Tests GetCredentials with non-existent agent.

**Verifies:**
- Returns ErrCredentialsNotFound
- Proper error type checking works
- Error messages are clear

### Test 8: Spawn Failure

Tests spawning agent without credentials.

**Verifies:**
- Spawning fails without credentials
- Error message is descriptive
- Manager handles errors gracefully

### Test 9: Cleanup

Stops all spawned agents.

**Verifies:**
- All agents can be stopped
- No resources leaked
- Clean shutdown

## Prerequisites

### Optional: Ollama

For LLM testing:
```bash
# Install Ollama
curl -fsSL https://ollama.com/install.sh | sh

# Start Ollama
ollama serve

# Pull model
ollama pull llama3.2
```

Note: Tests will run without Ollama, but LLM connection will fail.

## Configuration

### Test Agents

The tool uses these hardcoded test agents:

| Agent ID | Username | Password | Empire |
|----------|----------|----------|---------|
| explorer-7 | explorer-7-user | password-abc-123 | voidborn |
| miner-2 | miner-2-user | password-def-456 | voidborn |
| trader-1 | trader-1-user | password-ghi-789 | voidborn |

### Personality Templates

Minimal personalities are generated for testing:
- ID: From test agents
- Name: Same as ID
- Role: explorer (or from test)
- Faction: voidborn
- Traits: curiosity=0.8 (or from test)
- Skills: Basic set

## Usage in Development

### Before Adding Agents

```bash
# Test agent manager
go run ./cmd/test-agent-manager

# If tests pass, add new agents
# Update agent manager configuration
```

### After Credential Changes

```bash
# Test credential provider
go run ./cmd/test-agent-manager

# Verify credential retrieval works
```

### Continuous Integration

Add to CI pipeline:
```bash
#!/bin/bash
set -e

echo "Running agent manager integration tests..."
go run ./cmd/test-agent-manager

echo "All tests passed!"
```

## Troubleshooting

### Issue: "Failed to create temp dir"

**Cause:** Permission issue or disk full.

**Solution:**
1. Check /tmp permissions
2. Verify disk space
3. Set TEMP_DIR environment variable

### Issue: "LLM connection failed"

**Cause:** Ollama not running (expected in tests).

**Solution:**
- This is expected if Ollama isn't running
- Tests should continue and pass
- Only affects LLM-specific functionality

### Issue: "Failed to spawn agent"

**Cause:** Personality invalid or other issue.

**Solution:**
1. Check test personality templates
2. Verify agent manager code
3. Review error messages

### Issue: "Cleanup failed"

**Cause:** Agents didn't stop properly.

**Solution:**
1. Check for stuck processes
2. Kill orphaned agents
3. Review agent shutdown code

## Best Practices

### Run Before Deployment

```bash
# Always run tests before deploying agents
go run ./cmd/test-agent-manager

# Only deploy if tests pass
if [ $? -eq 0 ]; then
    echo "Deploying agents..."
fi
```

### Run After Changes

```bash
# After credential provider changes
go run ./cmd/test-agent-manager

# After agent manager changes
go run ./cmd/test-agent-manager

# After personality changes
go run ./cmd/test-agent-manager
```

### Isolated Testing

Each test run uses a unique temp directory:
```bash
# Multiple test runs won't interfere
go run ./cmd/test-agent-manager &
go run ./cmd/test-agent-manager &
```

## Related Tools

- [test-agent](../test-agent/) - Single agent testing
- [Agent Manager](../../pkg/agent/manager.go) - Manager implementation
- [Credential Provider](../../pkg/credentials/) - Credential storage

## License

Part of the SpaceMolt project.
