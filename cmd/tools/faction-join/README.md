# SpaceMolt Faction Join

> Faction management tool for creating factions, inviting members, and managing faction membership.

## Overview

The faction-join tool provides command-line utilities for managing SpaceMolt factions. It allows you to create factions, invite players, accept invitations, and view faction information. This tool is designed for testing faction mechanics and managing faction membership.

## Features

### Core Functionality
- **🏴 Create Faction** - Create a new faction with a name and tag
- **✉️ Send Invites** - Invite players to join your faction
- **✅ Accept Invites** - Accept faction invitations
- **ℹ️ View Info** - Display faction information for a player
- **👥 Multi-Agent** - Manage factions across multiple pirate agents

## Quick Start

### Basic Usage

```bash
# Create a faction (using pirate-1 as leader)
go run ./cmd/faction-join create

# Invite a player (pirate-1 invites pirate-2)
go run ./cmd/faction-join invite 2

# View faction info
go run ./cmd/faction-join info 1

# List available commands
go run ./cmd/faction-join
```

### Building

```bash
# Build the binary
go build -o bin/faction-join ./cmd/faction-join

# Run the built binary
./bin/faction-join create
```

## Commands

### create

Create a new faction using pirate-1 as the faction leader.

**Usage:**
```bash
go run ./cmd/faction-join create
```

**What it does:**
- Connects as pirate-1
- Sends create_faction command
- Creates "Crimson Corsairs" faction with tag "CRCO"

**Output:**
```
✓ Faction creation command sent
```

### invite

Invite a player to the faction. Uses pirate-1 to send the invitation.

**Usage:**
```bash
go run ./cmd/faction-join invite <pirate-num>
```

**Example:**
```bash
# Invite pirate-2 to the faction
go run ./cmd/faction-join invite 2
```

**What it does:**
- Loads target pirate's credentials
- Connects as pirate-1
- Sends faction_invite command with target's username

**Output:**
```
✓ Invitation sent to pirate-2-user
```

### join

Accept a faction invitation (currently requires manual faction_id).

**Usage:**
```bash
go run ./cmd/faction-join join <pirate-num>
```

**Example:**
```bash
# Accept invitation as pirate-2
go run ./cmd/faction-join join 2
```

**What it does:**
- Connects as specified pirate
- Retrieves pending invitations
- Prompts for manual faction_id (current limitation)

**Note:** This command is incomplete and requires manual intervention to specify the faction_id.

### info

Display faction information for a player.

**Usage:**
```bash
go run ./cmd/faction-join info <pirate-num>
```

**Example:**
```bash
# View faction info for pirate-1
go run ./cmd/faction-join info 1
```

**Output:**
```
Faction ID: crc_faction_12345
Faction Rank: Leader
```

## Examples

### Example 1: Creating a Faction

```bash
go run ./cmd/faction-join create
```

**Process:**
1. Connects as pirate-1
2. Creates "Crimson Corsairs" faction
3. pirate-1 becomes faction leader

### Example 2: Building a Faction

```bash
# Create faction
go run ./cmd/faction-join create

# Invite members
go run ./cmd/faction-join invite 2
go run ./cmd/faction-join invite 3
go run ./cmd/faction-join invite 4

# Accept invitations (on each member's account)
go run ./cmd/faction-join join 2
go run ./cmd/faction-join join 3
go run ./cmd/faction-join join 4

# Verify membership
go run ./cmd/faction-join info 1
go run ./cmd/faction-join info 2
```

### Example 3: Checking Faction Status

```bash
# Check leader's faction info
go run ./cmd/faction-join info 1

# Check member's faction info
go run ./cmd/faction-join info 2
```

## Configuration

### Agent Directory Structure

```
data/agents/
├── pirate-1/
│   └── credentials.json
├── pirate-2/
│   └── credentials.json
└── pirate-3/
    └── credentials.json
```

### Credentials Format

```json
{
  "username": "pirate-1-user",
  "password": "pirate-1-password",
  "empire": "voidborn"
}
```

### Hardcoded Faction Details

The tool has hardcoded faction details:
- **Name:** "Crimson Corsairs"
- **Tag:** "CRCO"

To change these, modify the values in `main.go` (lines 85-86).

## Limitations

### Current Limitations

- **Hardcoded Faction** - Only creates "Crimson Corsairs"
- **Pirate-1 as Leader** - Faction leader is always pirate-1
- **Incomplete Join** - join command requires manual faction_id
- **No Error Handling** - Limited error checking
- **No List Command** - Can't list all factions or invitations

### Future Enhancements

Potential improvements:
- Add CLI arguments for faction name/tag
- Implement proper invitation acceptance
- Add list command for pending invitations
- Add leave faction command
- Support for multiple factions
- Better error handling and validation

## Protocol Messages

### create_faction

```json
{
  "type": "create_faction",
  "payload": {
    "name": "Crimson Corsairs",
    "tag": "CRCO"
  },
  "timestamp": 1737657890123
}
```

### faction_invite

```json
{
  "type": "faction_invite",
  "payload": {
    "player_id": "pirate-2-user"
  },
  "timestamp": 1737657890123
}
```

### faction_get_invites

```json
{
  "type": "faction_get_invites",
  "timestamp": 1737657890123
}
```

### join_faction

```json
{
  "type": "join_faction",
  "payload": {
    "faction_id": "crc_faction_12345"
  },
  "timestamp": 1737657890123
}
```

### faction_info

```json
{
  "type": "faction_info",
  "timestamp": 1737657890123
}
```

## Troubleshooting

### Issue: "Failed to connect pirate-N"

**Cause:** Pirate credentials not found or invalid.

**Solution:**
1. Verify pirate-N directory exists
2. Check credentials.json file
3. Ensure pirate-N is a valid agent

### Issue: "Failed to create faction"

**Cause:** pirate-1 already in a faction or server error.

**Solution:**
1. Check if pirate-1 is already in a faction
2. Verify server is responding
3. Check faction name is available

### Issue: "Failed to send invite"

**Cause:** Target player not found or already invited.

**Solution:**
1. Verify target pirate exists
2. Check if target is already in faction
3. Ensure pirate-1 has permission to invite

### Issue: "join command incomplete"

**Cause:** join command requires manual faction_id input.

**Solution:**
1. Use faction_get_invites to list invitations
2. Manually send join_faction with faction_id
3. Or use play-simple for interactive joining

## Related Tools

- [play-simple](../play-simple/) - Interactive agent control
- [claim-code](../claim-code/) - Registration code claiming
- [Game Protocol](../../internal/protocol/) - Protocol message definitions

## License

Part of the SpaceMolt project.
