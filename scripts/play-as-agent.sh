#!/bin/bash
# play-as-agent.sh - Play Spacemolt using a given agent_id
# Usage: ./play-as-agent.sh <agent_id>

set -e

# Check if agent_id is provided
if [ -z "$1" ]; then
    echo "Usage: $0 <agent_id>"
    echo ""
    echo "Examples:"
    echo "  $0 miner-1"
    echo "  $0 explorer-5"
    echo "  $0 trader-3"
    echo ""
    echo "This script will:"
    echo "  1. Change to data/agents/{agent_id}/ directory"
    echo "  2. Read credentials from credentials.json"
    echo "  3. Launch the Spacemolt client with those credentials"
    exit 1
fi

AGENT_ID="$1"
AGENT_DIR="data/agents/${AGENT_ID}"
CREDENTIALS_FILE="${AGENT_DIR}/credentials.json"
CLIENT_BIN="$HOME/spacemolt/clients/spacemolt-client-linux-x64"

# Check if agent directory exists
if [ ! -d "$AGENT_DIR" ]; then
    echo "Error: Agent directory not found: $AGENT_DIR"
    echo ""
    echo "Available agents:"
    ls -1 data/agents/ | grep -E '^[a-z]+-[0-9]+$' | sort | head -20
    echo "..."
    exit 1
fi

# Check if credentials file exists
if [ ! -f "$CREDENTIALS_FILE" ]; then
    echo "Error: Credentials file not found: $CREDENTIALS_FILE"
    exit 1
fi

# Check if client binary exists
if [ ! -e "$CLIENT_BIN" ]; then
    echo "Error: Client binary not found: $CLIENT_BIN"
    exit 1
fi

# Read credentials from JSON file
# Using jq if available, otherwise fallback to grep/sed
if command -v jq &> /dev/null; then
    USERNAME=$(jq -r '.username' "$CREDENTIALS_FILE")
    PASSWORD=$(jq -r '.password' "$CREDENTIALS_FILE")
    EMPIRE=$(jq -r '.empire' "$CREDENTIALS_FILE")
else
    # Fallback: simple grep/sed parsing (less robust)
    USERNAME=$(grep '"username"' "$CREDENTIALS_FILE" | sed 's/.*"username"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    PASSWORD=$(grep '"password"' "$CREDENTIALS_FILE" | sed 's/.*"password"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    EMPIRE=$(grep '"empire"' "$CREDENTIALS_FILE" | sed 's/.*"empire"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
fi

# Validate credentials were extracted
if [ -z "$USERNAME" ] || [ -z "$PASSWORD" ]; then
    echo "Error: Failed to extract username/password from credentials file"
    exit 1
fi

# Change to agent directory
cd "$AGENT_DIR" || exit 1

# Display connection info
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║         Launching Spacemolt Client                        ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo "Agent ID    : $AGENT_ID"
echo "Username    : $USERNAME"
echo "Empire      : ${EMPIRE:-unknown}"
echo "Working Dir : $(pwd)"
echo "Client      : $CLIENT_BIN"
echo "╔═══════════════════════════════════════════════════════════╗"
echo ""

# Launch the client with credentials
# The client expects username and password as arguments
exec "$CLIENT_BIN" login "$USERNAME" "$PASSWORD"
echo "╚═══════════════════════════════════════════════════════════╝"

# Change to agent dir so we are ready to play.
cd ${AGENT_DIR}

