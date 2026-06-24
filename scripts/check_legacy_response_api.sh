#!/usr/bin/env bash
#
# Fails if new call sites to the legacy response API appear outside the
# migration allowlist. Shrink the allowlist as methods migrate; when it's
# empty, delete this script along with Client.Send (rename to send),
# waitForResponse, waitForActionResponse, and pkg/game/client_queue.go.
#
# Legacy entry points (any reference outside allowlist = failure):
#   - c.Send(  / client.Send(   (direct fire-and-forget send)
#   - waitForResponse(           (single-slot type-keyed waiter)
#   - waitForActionResponse(     (multi-type action waiter)
#   - CommandQueue ... Enqueue   (serialized legacy queue)

set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# Files allowed to retain legacy calls until they migrate. Delete entries as
# you migrate methods. Comments after '#' document why each file is here.
ALLOWLIST=(
    # Router internals — Send is the underlying wire primitive used by the
    # exec primitives. When migration completes, Send is renamed to lowercase
    # send and this entry stays.
    pkg/game/client.go
    pkg/game/client_queue.go
    pkg/game/response_exec.go

    # Unmigrated method bodies — drop entries as batches 1-4 migrate.
    # client_commands.go: remaining bare c.Send() fire-and-forget methods
    # (CommissionQuote, CommissionStatus, Chat, Forum*, CaptainsLog*, Notes*,
    # Catalog helpers, FactionList, FactionEdit*, FactionQueryIntel,
    # FactionRooms*, etc.). These intentionally have no server-ack classifier.
    # Also: GetBattleStatus (deferred from Batch 3).
    pkg/game/client_commands.go
    pkg/game/crafting.go

    # Tests exercising the legacy waiter / queue paths directly.
    pkg/game/client_test.go
    pkg/game/client_queue_test.go

    # External tools that intentionally send arbitrary/raw commands —
    # no migrated Client method can replace them.
    # play-simple: interactive REPL that sends user-typed command strings.
    cmd/debug/play-simple/main.go
    # server-cmd: one-shot tool that sends an arbitrary --cmd flag value.
    cmd/tools/server-cmd/main.go

    # Observer session — passes raw protocol.Message from browser clients
    # through gameClient.Send; the message type is only known at runtime
    # so no static classifier can be registered. Needs design work.
    pkg/observe/session.go

    # Overmind supervisor — srv.Send() is the Server.Send method (routes an
    # envelope to a worker via a Unix socket), not the game client legacy Send.
    pkg/overmind/supervisor/server.go
    pkg/overmind/supervisor/server_test.go
    # Overmind main — calls srv.Send() which is supervisor.Server.Send (same as above).
    cmd/overmind/main.go
    # Overmind integration test — calls srv.Send() which is supervisor.Server.Send (same as above).
    cmd/overmind/integration_test.go
    # Overmind task store — sender.Send() is the tasks.Sender interface (routes a
    # control envelope to a worker), not the game client legacy Send.
    pkg/overmind/tasks/store.go
    pkg/overmind/tasks/store_test.go
)

# Build a path filter: search the listed roots, exclude the allowlisted
# files/directories. We use git grep because it understands .gitignore and
# is fast on large checkouts.
# Match any receiver dotted-path ending in `.Send(` so e.g. `gameClient.Send`,
# `s.gameClient.Send`, `self.client.Send` are all caught — not just the bare
# `c.Send` / `client.Send` / `q.client.Send` forms.
PATTERN='\b[A-Za-z_][A-Za-z0-9_.]*\.Send\(|\bwaitForResponse\(|\bwaitForActionResponse\(|\bCommandQueue.*\.Enqueue\(|\bCmdQueue\.Enqueue\('

# Build pathspec excludes (one per allowlist entry).
EXCLUDES=()
for entry in "${ALLOWLIST[@]}"; do
    if [[ -d "$entry" ]]; then
        EXCLUDES+=( ":(exclude)$entry/**" )
    else
        EXCLUDES+=( ":(exclude)$entry" )
    fi
done

# Search only Go source under pkg/, cmd/, internal/.
SEARCH_PATHS=( 'pkg/**.go' 'cmd/**.go' 'internal/**.go' )

# shellcheck disable=SC2068  # we want word splitting on the arrays
HITS=$(git grep -nE "$PATTERN" -- ${SEARCH_PATHS[@]} ${EXCLUDES[@]} 2>/dev/null || true)

if [[ -n "$HITS" ]]; then
    echo "✗ New legacy response-API call detected outside allowlist:" >&2
    echo "$HITS" >&2
    echo >&2
    echo "Use c.execQuery / c.execMutation / c.subscribePush instead. See" >&2
    echo "docs/superpowers/specs/2026-04-24-response-router-design.md." >&2
    echo "If this file is being actively migrated, add it to the ALLOWLIST" >&2
    echo "in scripts/check_legacy_response_api.sh -- temporarily." >&2
    exit 1
fi

echo "✓ No new legacy response-API calls."
