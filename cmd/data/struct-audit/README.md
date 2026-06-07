# struct-audit

Audits how well our Go decode structs match the live game API. Compares three
sources, flattened to dotted JSON paths:

1. **Go** — structs in `pkg/game/serverapi/` and the `cmd/data/import-*` KB
   importers (dumped by this command via `go/ast`, nested types expanded).
2. **Spec** — `server_docs/openapi.json` component schemas.
3. **Live** — real response snapshots in `data/game-api/latest/*.json`.

## Run

From the repo root:

```sh
python3 cmd/data/struct-audit/audit.py
```

Writes `server_docs/import_struct_audit.md` (gitignored). Re-run after each data
scrape — the `openapi.json` symlink and `latest/` snapshots update, so the report
always reflects current drift.

The Go half can be run on its own to dump structs as JSON:

```sh
go run ./cmd/data/struct-audit pkg/game/serverapi
```

## Report sections

- **A** — fields in live data the Go struct lacks (add these).
- **B** — spec-declared fields Go lacks, with no live sample (lower confidence).
- **C** — structs with no snapshot, Go-vs-spec only (2-way).
- **D / D2** — Go fields gone from spec+live (candidate removals); D2 is
  unconfirmed (the snapshot's parent array/object was empty so it can't verify).
- **E** — `action` echo field (WebSocket-only; usually harmless).
- **F** — `cmd/data/import-*` importers vs their catalog/base/map snapshots.

## Confidence notes

- Snapshots/openapi describe the **HTTP REST** API. Go also decodes the
  **WebSocket** protocol, which bundles extra context (`action`, `nearby`,
  `poi`, `current_tick`) — these are reported separately, not as deletions.
- Go `map[string]...` fields cover their dynamic subkeys (no false "missing").
- A "stale" field is only *confirmed* when its parent array/object is populated
  in the live snapshot (empty arrays can't prove a field was removed).
