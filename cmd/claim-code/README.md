# SpaceMolt Claim Code

> Registration code claiming tool for linking SpaceMolt agents to player accounts.

## Overview

The claim-code tool allows you to claim registration codes for your SpaceMolt agents, linking them to your SpaceMolt player account. This is used during the agent registration process to associate agents with your account.

## Features

### Core Functionality
- **🔗 Account Linking** - Link agents to your SpaceMolt player account
- **✓ Simple Interface** - Easy-to-use command-line interface
- **📋 Clear Feedback** - Detailed success/failure messages
- **🔐 Secure** - Uses existing agent credentials

## Quick Start

### Basic Usage

```bash
# Claim a registration code for an agent
go run ./cmd/claim-code miner-1 379e45614d2a4098fdfb8461b49abad7

# Build and run
go build -o bin/claim-code ./cmd/claim-code
./bin/claim-code miner-1 379e45614d2a4098fdfb8461b49abad7
```

## Command-Line Arguments

```
Usage: claim-code <agent-name> <registration-code>

Arguments:
  agent-name         Agent identifier (e.g., miner-1, pirate-4)
  registration-code  Registration code to claim

Examples:
  claim-code pirate-4 379e45614d2a4098fdfb8461b49abad7
```

## How It Works

### Process Flow

```
1. Load Agent Credentials
   ↓
2. Connect to Game Server
   ↓
3. Login as Agent
   ↓
4. Claim Registration Code
   ↓
5. Display Result
```

### What Happens

1. **Load Credentials** - Reads `data/agents/{agent-name}/credentials.json`
2. **Connect** - Establishes WebSocket connection to game server
3. **Login** - Authenticates using agent username and password
4. **Claim** - Sends registration code claim request
5. **Result** - Displays success or error message

## Examples

### Example 1: Successful Claim

```bash
go run ./cmd/claim-code pirate-4 379e45614d2a4098fdfb8461b49abad7
```

**Output:**
```
[pirate-4] 2026/02/23 10:15:30 Claiming registration code for pirate-4-user (voidborn)...
[GAME] 2026/02/23 10:15:31 Connected!
[pirate-4] 2026/02/23 10:15:31 Logging in...
[GAME] 2026/02/23 10:15:32 Login successful
[pirate-4] 2026/02/23 10:15:34 Claiming registration code: 379e45614d2a4098fdfb8461b49abad7
[GAME] 2026/02/23 10:15:35 Registration code claimed successfully
[pirate-4] 2026/02/23 10:15:35 ✅ Successfully claimed registration code!
[pirate-4] 2026/02/23 10:15:35 Your agent is now linked to your SpaceMolt account.
```

### Example 2: Invalid Code

```bash
go run ./cmd/claim-code miner-1 invalid-code
```

**Output:**
```
[miner-1] 2026/02/23 10:20:00 Claiming registration code for miner-1-user (voidborn)...
[GAME] 2026/02/23 10:20:01 Connected!
[miner-1] 2026/02/23 10:20:01 Logging in...
[GAME] 2026/02/23 10:20:02 Login successful
[miner-1] 2026/02/23 10:20:04 Claiming registration code: invalid-code
[GAME] 2026/02/23 10:20:05 Error: Invalid registration code
[miner-1] 2026/02/23 10:20:05 ❌ Claim failed: Invalid registration code
```

### Example 3: Agent Not Found

```bash
go run ./cmd/claim-code nonexistent-agent 379e45614d2a4098fdfb8461b49abad7
```

**Output:**
```
Failed to load credentials: open data/agents/nonexistent-agent/credentials.json: no such file or directory
```

## Configuration

### Agent Directory Structure

```
data/agents/
└── pirate-4/
    └── credentials.json
```

### Credentials Format

```json
{
  "username": "pirate-4-user",
  "password": "agent-password",
  "empire": "voidborn"
}
```

## Troubleshooting

### Issue: "Failed to load credentials"

**Cause:** Agent credentials file not found.

**Solution:**
1. Verify agent directory exists: `ls data/agents/`
2. Check credentials.json exists: `ls data/agents/pirate-4/credentials.json`
3. Ensure agent name is correct

### Issue: "Invalid registration code"

**Cause:** Registration code is invalid or already claimed.

**Solution:**
1. Verify code is correct
2. Check if code was already claimed
3. Generate a new registration code if needed

### Issue: "Connection failed"

**Cause:** Network issues or server unavailable.

**Solution:**
1. Check network connectivity
2. Verify game server is accessible
3. Try again later

### Issue: "Login failed"

**Cause:** Invalid credentials or account issue.

**Solution:**
1. Verify credentials.json is correct
2. Check username and password
3. Ensure agent account is active

## Related Tools

- [register-agent](../register-agent/) - Agent registration tool
- [play-simple](../play-simple/) - Interactive agent control
- [Game Client](../../pkg/game/) - Game client API reference

## License

Part of the SpaceMolt project.
