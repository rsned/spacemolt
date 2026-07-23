// Package buildinfo is the single source of a running binary's build identity:
// a human-readable version label plus the embedded git commit, build time, and
// dirty flags. It is a leaf package (no spacemolt imports) so every binary can
// import it cheaply.
//
// This SemVer axis tracks fleet *binary* builds. It is deliberately separate
// from pkg/version (VersionID, BuiltForAPIVersion), which tracks the game-server
// API the client targets. The two are not unified.
package buildinfo

import (
	"runtime/debug"
	"sync"
	"time"
)

// version is set at build time via
//
//	-ldflags "-X github.com/rsned/spacemolt/pkg/buildinfo.version=$(git describe --tags --always --dirty)"
//
// A plain `go build` leaves it empty; resolve falls back to the module
// pseudo-version, then the literal "dev". See scripts/build.sh.
var version string

// codeDirty is set at build time via
//
//	-X github.com/rsned/spacemolt/pkg/buildinfo.codeDirty=true|false
//
// It reflects uncommitted tracked changes OUTSIDE data/ (data/*.json churns
// constantly, so raw vcs.modified is unusable for coloring). Unset ⇒ false.
var codeDirty string

// Info identifies the binary that is running.
type Info struct {
	Version   string    // git describe label, module pseudo-version, or "dev"
	Commit    string    // short vcs.revision, "" if unavailable
	BuiltAt   time.Time // vcs.time, zero if unavailable
	Modified  bool      // raw vcs.modified — cosmetic only (noisy from data/*.json)
	CodeDirty bool      // uncommitted tracked code outside data/ — color-relevant
}

var (
	once   sync.Once
	cached Info
)

// Get returns the running binary's build identity. It reads
// runtime/debug.ReadBuildInfo once, memoizes the result, and never panics.
func Get() Info {
	once.Do(func() {
		bi, ok := debug.ReadBuildInfo()
		cached = resolve(version, codeDirty, bi, ok)
	})
	return cached
}

// resolve builds an Info from the ldflags vars and a (possibly nil) build info.
// Split out from Get so tests can exercise the fallback ladder deterministically
// without depending on how the test binary itself was built.
func resolve(version, codeDirty string, bi *debug.BuildInfo, ok bool) Info {
	info := Info{Version: version, CodeDirty: codeDirty == "true"}
	if ok && bi != nil {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Commit = shortCommit(s.Value)
			case "vcs.time":
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					info.BuiltAt = t
				}
			case "vcs.modified":
				info.Modified = s.Value == "true"
			}
		}
		if info.Version == "" {
			if v := bi.Main.Version; v != "" && v != "(devel)" {
				info.Version = v
			}
		}
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}

func shortCommit(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return rev
}
