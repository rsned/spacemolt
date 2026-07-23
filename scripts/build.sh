#!/usr/bin/env bash
# Build the fleet binaries into bin/ with build-identity stamping.
#
# Stamps two ldflags vars in pkg/buildinfo:
#   version   = git describe --tags --always --dirty (SemVer label + commit)
#   codeDirty = whether tracked files OUTSIDE data/ have uncommitted changes
#               (data/*.json churns constantly, so raw vcs.modified is unusable
#                for coloring).
#
# A plain `go build ./...` still works and yields version "dev" with codeDirty
# unset — this script is for release builds, not a hard requirement.
#
# For a recursive dev build of every cmd/* tool (with -race, no stamping), see
# scripts/build-all.sh.
set -euo pipefail

cd "$(dirname "$0")/.."

DESC=$(git describe --tags --always --dirty)
if [ -z "$(git status --porcelain -- ':!data/')" ]; then
  CODEDIRTY=false
else
  CODEDIRTY=true
fi

LDFLAGS="-X github.com/rsned/spacemolt/pkg/buildinfo.version=$DESC \
-X github.com/rsned/spacemolt/pkg/buildinfo.codeDirty=$CODEDIRTY"

mkdir -p bin
go build -ldflags "$LDFLAGS" -o bin/overmind ./cmd/overmind
go build -ldflags "$LDFLAGS" -o bin/worker ./cmd/worker
go build -ldflags "$LDFLAGS" -o bin/overmind-dashboard ./cmd/overmind-dashboard

echo "built bin/overmind bin/worker bin/overmind-dashboard @ $DESC (codeDirty=$CODEDIRTY)"
