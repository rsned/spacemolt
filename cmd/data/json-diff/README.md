# JSON Diff

Compare two JSON files with intelligent normalization for meaningful diffs.

## Overview

The `json-diff` tool normalizes two JSON files and compares them using `diff`. It sorts object keys recursively and intelligently sorts arrays of objects by their identity fields, making it easy to see actual differences rather than formatting noise.

## Features

- **Key Sorting**: Recursively sorts all object keys alphabetically
- **Array Sorting**: Intelligently sorts arrays of objects by identity fields
- **Automatic Field Detection**: Tries common identity fields (`id`, `system_id`, `name`, etc.)
- **Manual Key Override**: Force a specific field with `-key`
- **Colored Output**: Uses `diff --color=auto` for readable output
- **Temporary Files**: Cleans up temporary normalized files automatically

## Usage

### Basic Usage

```bash
# Compare two JSON files
go run ./cmd/json-diff file1.json file2.json

# Specify a custom key for sorting arrays
go run ./cmd/json-diff -key custom_id file1.json file2.json

# Build and use binary
go build -o bin/json-diff ./cmd/json-diff
./bin/json-diff data1.json data2.json
```

### Command-Line Flags

```
-key string
    Force this field as the sort key for arrays of objects
    (default: auto-detect from common fields)

Examples:
    -key system_id
    -key item_id
    -key name
```

### How It Works

1. **Load JSON**: Reads both JSON files
2. **Normalize**: Sorts object keys and arrays
3. **Write Temp Files**: Saves normalized versions to temp directory
4. **Run Diff**: Executes `diff -u --color=auto` on normalized files
5. **Clean Up**: Removes temporary files

## Array Sorting

The tool automatically detects identity fields in this order:

1. `id`
2. `system_id`
3. `item_id`
4. `poi_id`
5. `base_id`
6. `name`
7. `key`
8. `type`
9. `resource_id`

If none of these fields exist, arrays are left unsorted.

### Example: Array Sorting

**Before** (unsorted, hard to diff):
```json
[
  {"name": "charlie", "id": "3"},
  {"name": "alpha", "id": "1"},
  {"name": "bravo", "id": "2"}
]
```

**After** (sorted by `id`):
```json
[
  {"id": "1", "name": "alpha"},
  {"id": "2", "name": "bravo"},
  {"id": "3", "name": "charlie"}
]
```

## Example: Comparing Game Data

```bash
# Compare two snapshots of game systems
go run ./cmd/json-diff \
  server_docs/systems.20260201.json \
  server_docs/systems.20260216.json
```

**Output**:
```diff
--- server_docs/systems.20260201.json
+++ server_docs/systems.20260216.json
@@ -124,7 +124,7 @@
       },
       {
         "id": "system_alpha_3",
-        "police_level": 0.5,
+        "police_level": 0.75,
         "security_status": "medium"
       }
     ]
```

## Use Cases

### 1. API Response Comparison

Compare game API responses at different times:

```bash
# Download current API docs
curl -o new.json https://api.spacemolt.com/v1/systems

# Compare with previous version
go run ./cmd/json-diff old.json new.json
```

### 2. Data Migration Validation

Verify data integrity after migration:

```bash
# Export from old database
./export-old > old-format.json

# Export from new database
./export-new > new-format.json

# Compare (should be identical except for timestamps)
go run ./cmd/json-diff old-format.json new-format.json
```

### 3. Configuration Validation

Compare config files:

```bash
go run ./cmd/json-diff production.json staging.json
```

### 4. Test Output Comparison

Compare test results:

```bash
go run ./cmd/json-diff test-results/golden.json test-results/actual.json
```

## Exit Codes

- `0`: Files are identical
- `1`: Files differ (normal, expected for diff tool)
- `2`: Error occurred (invalid JSON, missing files, etc.)

## Building

```bash
# Build the binary
go build -o bin/json-diff ./cmd/json-diff

# Run the built binary
./bin/json-diff file1.json file2.json
```

## Requirements

- `diff` command must be available in PATH
- Most Unix-like systems include `diff` by default
- On Windows, use WSL or Git Bash

## Implementation Details

### Normalization Process

1. **Parse JSON**: Unmarshal into `interface{}`
2. **Sort Objects**: Recursively sort all map keys
3. **Sort Arrays**: Detect identity field and sort by it
4. **Marshal**: Re-marshal with 2-space indentation
5. **Add Newline**: Ensure trailing newline for diff compatibility

### Identity Field Detection

```go
var candidateKeys = []string{
    "id", "system_id", "item_id", "poi_id", "base_id",
    "name", "key", "type", "resource_id",
}
```

The first matching key is used. If `-key` is specified, it overrides automatic detection.

### Temporary File Handling

- Created in `/tmp/json-diff-*` (or system temp directory)
- Automatically cleaned up on exit
- Disambiguated if both files have the same basename:
  - `a_filename.json`
  - `b_filename.json`

## Troubleshooting

### Issue: "diff: command not found"

**Solution**: Install `diff` utility:
- Debian/Ubuntu: `sudo apt-get install diffutils`
- macOS: Included (Xcode command line tools)
- Windows: Use WSL or Git Bash

### Issue: Arrays not sorting correctly

**Solution**: Specify the identity field explicitly:

```bash
go run ./cmd/json-diff -key your_custom_field file1.json file2.json
```

### Issue: "invalid JSON" error

**Solution**: Validate JSON files first:

```bash
# Check if JSON is valid
jq '.' file.json

# Or Python
python3 -m json.tool file.json > /dev/null
```

## Related Tools

- [jq](https://stedolan.github.io/jq/) - JSON processor and query language
- [jd](https://github.com/josephburnett/jd) - JSON diff and patch
- [diff](https://man7.org/linux/man-pages/man1/diff.1.html) - Standard diff utility

## License

Part of the SpaceMolt project.
