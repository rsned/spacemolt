package ovdash

import (
	"time"

	"github.com/rsned/spacemolt/pkg/version"
)

// Tier is a build-freshness color relative to the current (newest) fleet build.
type Tier string

const (
	TierGreen  Tier = "green"  // identical to current, code-clean
	TierYellow Tier = "yellow" // same major.minor but drifted, or code-dirty
	TierRed    Tier = "red"    // different minor/major, or legacy/unparseable
)

// buildSample is one (version, built_at) observation used to pick the current
// build. built_at is an RFC3339 string; unparseable/empty samples are ignored.
type buildSample struct {
	Version string
	BuiltAt string
}

// currentVersion returns the version string of the sample with the newest
// parseable built_at. Build-time — not SemVer — decides which build is current
// (monotonic, robust to out-of-order tags). Empty when nothing is datable.
func currentVersion(samples []buildSample) string {
	var best time.Time
	var bestVer string
	for _, s := range samples {
		if s.BuiltAt == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, s.BuiltAt)
		if err != nil {
			continue
		}
		if bestVer == "" || t.After(best) {
			best, bestVer = t, s.Version
		}
	}
	return bestVer
}

// Classify colors a build by SemVer distance from current. green = same version
// string and code-clean; yellow = same major.minor but patch/commit-behind or
// code-dirty; red = different minor/major, or an unparseable/legacy version.
func Classify(ver string, codeDirty bool, current string) Tier {
	v, errV := version.ParseSemVer(ver)
	cur, errCur := version.ParseSemVer(current)
	if errV != nil || errCur != nil {
		return TierRed
	}
	if v.Major != cur.Major || v.MinorDiff(cur) != 0 {
		return TierRed
	}
	if ver == current && !codeDirty {
		return TierGreen
	}
	return TierYellow
}

// worstTier returns the most severe tier present (red > yellow > green). No
// tiers ⇒ green.
func worstTier(tiers ...Tier) Tier {
	rank := map[Tier]int{TierGreen: 0, TierYellow: 1, TierRed: 2}
	worst := TierGreen
	for _, t := range tiers {
		if rank[t] > rank[worst] {
			worst = t
		}
	}
	return worst
}
