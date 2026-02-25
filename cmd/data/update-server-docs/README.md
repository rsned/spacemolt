# SpaceMolt Update Server Docs

> Automated documentation downloader for keeping SpaceMolt API documentation and OpenAPI specifications up-to-date.

## Overview

The update-server-docs tool downloads the latest API documentation and OpenAPI specifications from the SpaceMolt game server. It maintains dated archives and updates symlinks for easy access to current documentation. Essential for keeping development tools in sync with server API changes.

## Features

### Core Functionality
- **📥 Automatic Download** - Fetches docs from multiple URLs
- **📅 Date Stamping** - Archives docs with date stamps
- **🔗 Symlink Management** - Maintains symlinks to latest versions
- **🔄 Incremental Updates** - Only downloads if content changed
- **📊 Multiple Formats** - Downloads Markdown, JSON, and API docs

### Downloaded Resources

The tool fetches:
1. **skill.md** - Skill system documentation
2. **api.md** - API documentation
3. **openapi.json** - OpenAPI v1 specification
4. **api.v2.md** - API v2 documentation
5. **openapi.v2.json** - OpenAPI v2 specification

## Quick Start

### Basic Usage

```bash
# Download all latest documentation
go run ./cmd/update-server-docs

# Build and run
go build -o bin/update-server-docs ./cmd/update-server-docs
./bin/update-server-docs
```

## Examples

### Example 1: Initial Download

```bash
go run ./cmd/update-server-docs
```

**Output:**
```
skill.md: saved as skill.20260223.md
skill.md: symlink -> skill.20260223.md
api.md: saved as api.20260223.md
api.md: symlink -> api.20260223.md
openapi.json: saved as openapi.20260223.json
openapi.json: symlink -> openapi.20260223.json
api.v2.md: saved as api.v2.20260223.md
api.v2.md: symlink -> api.v2.20260223.md
openapi.v2.json: saved as openapi.v2.20260223.json
openapi.v2.json: symlink -> openapi.v2.20260223.json
Documentation updated successfully
```

### Example 2: No Changes

```bash
go run ./cmd/update-server-docs
```

**Output (if no changes):**
```
skill.md: already up to date
skill.md: symlink -> skill.20260223.md
api.md: already up to date
api.md: symlink -> api.20260223.md
...
Documentation updated successfully
```

### Example 3: After Server Update

```bash
# Server updated, run downloader
go run ./cmd/update-server-docs
```

**Output:**
```
skill.md: saved as skill.20260224.md
skill.md: symlink -> skill.20260224.md
api.md: saved as api.20260224.md
api.md: symlink -> api.20260224.md
...
Documentation updated successfully
```

## Directory Structure

After running, the `server_docs/` directory looks like:

```
server_docs/
├── skill.md -> skill.20260223.md
├── skill.20260223.md
├── skill.20260222.md
├── api.md -> api.20260223.md
├── api.20260223.md
├── api.20260222.md
├── openapi.json -> openapi.20260223.json
├── openapi.20260223.json
├── openapi.20260222.json
├── api.v2.md -> api.v2.20260223.md
├── api.v2.20260223.md
├── api.v2.20260222.md
├── openapi.v2.json -> openapi.v2.20260223.json
├── openapi.v2.20260223.json
└── openapi.v2.20260222.json
```

## URLs Fetched

The tool downloads from these URLs:

| File | URL |
|------|-----|
| skill.md | https://www.spacemolt.com/skill.md |
| api.md | https://www.spacemolt.com/api.md |
| openapi.json | https://game.spacemolt.com/api/openapi.json |
| api.v2.md | https://www.spacemolt.com/api/v2/docs |
| openapi.v2.json | https://game.spacemolt.com/api/v2/openapi.json |

## How It Works

### Process Flow

```
For each documentation file:
  1. Download from URL
  2. Create dated filename (basename.YYYYMMDD.ext)
  3. Check if content changed
  4. If changed: Write new dated file
  5. Update symlink to latest dated file
```

### Change Detection

The tool checks if content has changed:
1. Download new content
2. Read existing dated file (if exists)
3. Compare content byte-by-byte
4. Only write if different

### Symlink Management

For each file:
1. Remove existing symlink (if exists)
2. Create new symlink pointing to latest dated file
3. Ensures "filename" always points to latest version

## Usage in Development

### Update Before Code Generation

```bash
# 1. Update documentation
go run ./cmd/update-server-docs

# 2. Regenerate MCP tools
go run ./cmd/generate-mcp-tools

# 3. Rebuild tools
go build ./cmd/mcp-ws-bridge
```

### Regular Updates

Add to cron job or automation:
```bash
# Daily update at 9 AM
0 9 * * * cd /path/to/spacemolt && go run ./cmd/update-server-docs
```

### Pre-Commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit

# Update docs before commit
echo "Updating server documentation..."
go run ./cmd/update-server-docs

# Check if anything changed
if git diff --quiet server_docs/; then
    echo "No documentation changes"
else
    echo "Documentation updated, please review"
    git add server_docs/
fi
```

## Configuration

### Custom URLs

To change URLs, modify the constants in `main.go` (lines 13-19):

```go
const (
    skillURL      = "https://www.spacemolt.com/skill.md"
    apiURL        = "https://www.spacemolt.com/api.md"
    openAPIURL    = "https://game.spacemolt.com/api/openapi.json"
    apiV2URL      = "https://www.spacemolt.com/api/v2/docs"
    openAPIV2URL  = "https://game.spacemolt.com/api/v2/openapi.json"
)
```

### Custom Output Directory

To change output directory, modify (line 14):
```go
docsDir = "custom_docs"
```

## Integration

### With MCP Tool Generation

```bash
# Update workflow
go run ./cmd/update-server-docs        # 1. Get latest docs
go run ./cmd/generate-mcp-tools         # 2. Regenerate tools
go build ./cmd/mcp-ws-bridge            # 3. Rebuild
```

### With Documentation

```bash
# Always use symlinks in code
# Don't hardcode dates

# Good
docs_path = "server_docs/api.md"

# Bad
docs_path = "server_docs/api.20260223.md"
```

## Troubleshooting

### Issue: "HTTP status: 404 Not Found"

**Cause:** URL changed or server unavailable.

**Solution:**
1. Check if URL is correct
2. Verify server is accessible
3. Check browser for URL availability

### Issue: "Error creating docs directory"

**Cause:** Permission issue.

**Solution:**
1. Check write permissions
2. Create directory manually: `mkdir server_docs`
3. Check disk space

### Issue: "Error downloading"

**Cause:** Network issue or timeout.

**Solution:**
1. Check internet connection
2. Try again later
3. Verify URL is accessible in browser

### Issue: "Symlink creation failed"

**Cause:** Platform doesn't support symlinks or permission issue.

**Solution:**
1. On Windows: Run as administrator or enable developer mode
2. Check permissions
3. Verify filesystem supports symlinks

## Best Practices

### Regular Updates

Run regularly to keep docs current:
```bash
# Daily (cron)
0 9 * * * go run ./cmd/update-server-docs

# Weekly
0 9 * * 1 go run ./cmd/update-server-docs
```

### Version Control

Commit updated docs to track API changes:
```bash
# Update docs
go run ./cmd/update-server-docs

# Commit changes
git add server_docs/
git commit -m "Update server docs $(date +%Y%m%d)"
```

### Archive Management

Periodically clean old archives:
```bash
# Keep last 30 days
find server_docs/ -name "*.md" -mtime +30 -delete
find server_docs/ -name "*.json" -mtime +30 -delete
```

## Performance

### Typical Performance

- **Download Time** - 2-5 seconds (all files)
- **File Size** - ~500 KB total
- **Network** - ~1 MB transferred

### Bandwidth

- **Full Download** - All 5 files
- **Incremental** - Only changed files
- **Change Detection** - Byte-by-byte comparison

## Related Tools

- [generate-mcp-tools](../generate-mcp-tools/) - Generate MCP tools from OpenAPI specs
- [MCP WebSocket Bridge](../mcp-ws-bridge/) - Tool that uses generated code

## License

Part of the SpaceMolt project.
