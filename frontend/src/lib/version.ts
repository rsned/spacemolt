// shortVersion compacts a build string for display in the fixed-width fleet
// badges. Several long shapes occur in practice, and each blows the badge out
// wide enough to wrap the row and shove the credits figure off its baseline:
//
//  1. `git describe` when HEAD is not exactly on a tag:
//     `<tag>-<n>-g<hash>`, e.g. v0.2.4-9-g2c1212f
//  2. a Go module PSEUDO-VERSION, stamped when a binary carries no tag info:
//     `<base>-<seq>.<yyyymmddhhmmss>-<12 hex>`, e.g.
//     v0.2.6-0.20260728025332-29399a4cecfb. A plain `go build -o bin/worker`
//     produces these, so the freshest fleets — the ones just rolled — are
//     exactly the ones whose badges break.
//  3. either of the above with a `+dirty` build suffix, which is what a build
//     from a working tree with uncommitted changes actually looks like:
//     v0.2.6-0.20260728025332-29399a4cecfb+dirty
//
// The suffix is split off FIRST. Anchoring a pattern on the trailing hash means
// a suffixed build matches nothing and falls through to the raw-length cap,
// which is the whole bug: those are the longest strings of all.
//
// Callers keep the untouched full string in a title tooltip, and the commit
// hash is surfaced separately anyway. Anything unrecognised is passed through,
// then hard-capped so no unanticipated shape can break the layout again.
//
//   v0.2.4-9-g2c1212f                          -> v0.2.4+9
//   v0.2.6-0.20260728025332-29399a4cecfb       -> v0.2.6+dev
//   v0.2.6-0.20260728025332-29399a4cecfb+dirty -> v0.2.6+dirty
//   v0.0.0-20260727194500-1a2b3c4d5e6f         -> v0.0.0+dev
//   v0.2.5                                     -> v0.2.5
//   dev / legacy                               -> unchanged
const SUFFIX = /^(.*?)(\+[a-z]+)$/;
const DESCRIBE = /^(.+)-(\d+)-g[0-9a-f]+$/;
const PSEUDO = /^(.+?)-(?:\d+\.)?\d{14}-[0-9a-f]{12}$/;

export function shortVersion(v: string): string {
  const sfx = SUFFIX.exec(v);
  const base = sfx ? sfx[1] : v;
  const tail = sfx ? sfx[2] : '';

  let s = base + tail;
  const d = DESCRIBE.exec(base);
  const p = d ? null : PSEUDO.exec(base);
  if (d) {
    s = `${d[1]}+${d[2]}${tail}`;
  } else if (p) {
    // A dirty marker already says "not a release build", so it stands in for
    // the +dev marker rather than stacking with it.
    s = `${p[1]}${tail || '+dev'}`;
  }
  return s.length > 16 ? `${s.slice(0, 15)}…` : s;
}
