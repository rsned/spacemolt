#!/bin/bash
# Setup script to install cron job for daily documentation updates

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
UPDATE_SCRIPT="$PROJECT_DIR/update-server-docs"

# Check if the binary exists
if [ ! -f "$UPDATE_SCRIPT" ]; then
    echo "Error: $UPDATE_SCRIPT not found"
    echo "Please build it first with: go build -o update-server-docs ./cmd/update-server-docs"
    exit 1
fi

# Install cron job to run daily at 2 AM
CRON_JOB="0 2 * * * cd $PROJECT_DIR && ./update-server-docs >> server_docs/update.log 2>&1"

# Check if cron job already exists
if crontab -l 2>/dev/null | grep -q "update-server-docs"; then
    echo "Cron job for update-server-docs already exists"
    echo "Current crontab entry:"
    crontab -l 2>/dev/null | grep "update-server-docs"
else
    # Add new cron job
    (crontab -l 2>/dev/null; echo "$CRON_JOB") | crontab -
    echo "Cron job installed successfully"
    echo "Documentation will be updated daily at 2:00 AM"
fi

echo ""
echo "To run manually:"
echo "  cd $PROJECT_DIR && ./update-server-docs"
echo ""
echo "To view logs:"
echo "  cat $PROJECT_DIR/server_docs/update.log"
