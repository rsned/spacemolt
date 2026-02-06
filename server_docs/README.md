# Server Documentation Updater

This tool downloads and versions the latest Spacemolt server documentation.

## Files

- `skill.md` - Symlink to the latest skill documentation
- `api.md` - Symlink to the latest API documentation
- `skill.YYYYMMDD.md` - Dated versions of skill.md
- `api.YYYYMMDD.md` - Dated versions of api.md

## Usage

### Manual Update

Build and run the updater:

```bash
go build -o update-server-docs ./cmd/update-server-docs
./update-server-docs
```

### Automatic Daily Updates

Run the setup script to install a cron job:

```bash
./scripts/setup-docs-updater.sh
```

This will update the documentation daily at 2:00 AM.

To customize the schedule, edit your crontab:

```bash
crontab -e
```

### Accessing Documentation

Use the symlinked files for the latest version:

```bash
# Latest API docs
cat server_docs/api.md

# Latest skill docs
cat server_docs/skill.md

# View specific date
cat server_docs/api.20260205.md
```

## Removing Old Versions

To keep only recent versions (e.g., last 30 days):

```bash
find server_docs -name "*.md" ! -type l -mtime +30 -delete
```
