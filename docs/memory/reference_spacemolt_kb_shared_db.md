---
name: Shared spacemolt-knowledge.db with spacemolt-kb repo
description: Tables poi_metadata_planets and poi_metadata_stars in the live DB are owned by the separate spacemolt-kb repo, not dead code
type: reference
originSessionId: 46e1508c-94bc-41f8-aa4b-d5724c659e9c
---
The live `data/spacemolt-knowledge.db` (symlinked from
`/home/robert/spacemolt/spacemolt-knowledge.db`) is shared between this repo
and a separate project at `/home/robert/spacemolt/kb/`
(https://github.com/rsned/spacemolt-kb).

Tables created and owned by `spacemolt-kb`, NOT by this repo's migration
chain in `pkg/knowledge/sqlite_migrations.go`:

- `poi_metadata_planets` — planetary metadata (planet_class, radius, mass,
  gravity, temperature, atmosphere, orbital data)
- `poi_metadata_stars` — stellar metadata (star_class, spectral type,
  luminosity, color, size multiplier)

**Do not flag these as drift/phantom tables** when auditing the live DB
against this repo's migrations. They're intentionally absent from
`scripts/sql/initialize_database.sql` and from `sqlite_migrations.go` —
their creation lives in the `spacemolt-kb` repo's own schema/code.

Also: when regenerating `initialize_database.sql` via
`scripts/sql/regenerate_initialize_database.sh`, these tables will not
appear — that's correct.
