---
name: server-consolidation
description: Migrated all agent-server features to spacemolt-server for deprecation — model API, SSE streaming, agent detail endpoints, registry, credentials, auto-discovery
type: project
---

Migrated all 8 features from cmd/agent-server to cmd/spacemolt-server (unified server) on 2026-03-19.

**Why:** agent-server was the legacy entry point; spacemolt-server is the replacement with additional capabilities (strategies, teams, monitoring, frontend).

**How to apply:** cmd/agent-server can be considered deprecated. All new features should go into pkg/unified/ and cmd/spacemolt-server/. The LegacyProvider credentials backend was also removed.

Features migrated:
1. LLM model hot-swap API (`pkg/unified/model_api.go`)
2. SSE event streaming (`pkg/unified/stream_api.go`)
3. Agent detail/state/history endpoints (`pkg/unified/agent_api.go`)
4. Status registry integration (`pkg/unified/registry_api.go`)
5. Credential migration utility (`pkg/credentials/migrate.go`)
6. Agent auto-discovery from env/directory (`pkg/unified/config.go`)
7. Keyring credentials backend
8. Removed LegacyProvider
