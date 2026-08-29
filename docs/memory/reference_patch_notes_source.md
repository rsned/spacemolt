---
name: reference_patch_notes_source
description: Where to find Spacemolt server patch notes and the current server API version
metadata: 
  node_type: memory
  type: reference
  originSessionId: ca64e2a6-eb83-4db2-b98e-60306d83990c
---

Web changelog (paginated): **https://www.spacemolt.com/changelog**

The game also exposes patch notes and the current server version in-game:

- **`get_version`** (play_as alias: `version` / `get_version`, dispatched at
  `cmd/tools/play_as/main.go`) → `GetVersionResponse` with `version`,
  `release_date`, `notes []string`, and a paginated `versions []ChangelogVersion`
  changelog. This is the best way to read the current server version.
- **`search_changelog`** → `SearchChangelogResponse{ releases []ChangelogVersion }`.
- The **`welcome`** frame on login also carries `version` and `release_notes`.

Use `get_version` to read the current server version whenever bumping
`BuiltForAPIVersion` (see [[feedback_version_constant]]). The `server_docs/`
dated `api.md` / `openapi.json` files reflect API shape per download, not the
running server's live patch notes (see [[reference_server_docs_sync]]).
