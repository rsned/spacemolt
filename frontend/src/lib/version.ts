// shortVersion compacts a build string for display in the fixed-width fleet
// badges. Two long shapes occur in practice, and both blow the badge out wide
// enough to wrap the row and shove the credits figure off its baseline:
//
//  1. `git describe` when HEAD is not exactly on a tag:
//     `<tag>-<n>-g<hash>`, e.g. v0.2.4-9-g2c1212f
//  2. a Go module PSEUDO-VERSION, stamped when a binary carries no tag info:
//     `<base>-<seq>.<yyyymmddhhmmss>-<12 hex>`, e.g.
//     v0.2.6-0.20260727194500-1a2b3c4d5e6f. A plain `go build -o bin/worker`
//     produces these, so the freshest fleets — the ones just rolled — are
//     exactly the ones whose badges break.
//
// Both render as `<base>+<marker>`; callers keep the untouched full string in
// a title tooltip, and the commit hash is surfaced separately anyway.
//
// Anything else is passed through, then hard-capped so no unanticipated
// version string can break the layout again.
//
//   v0.2.4-9-g2c1212f                    -> v0.2.4+9
//   v0.2.4-123-gabcdef0                  -> v0.2.4+123
//   v0.2.6-0.20260727194500-1a2b3c4d5e6f -> v0.2.6+dev
//   v0.0.0-20260727194500-1a2b3c4d5e6f   -> v0.0.0+dev
//   v0.2.5                               -> v0.2.5
//   dev / legacy                         -> unchanged
const DESCRIBE = /^(.+)-(\d+)-g[0-9a-f]+$/;
const PSEUDO = /^(.+?)-(?:\d+\.)?\d{14}-[0-9a-f]{12}$/;

export function shortVersion(v: string): string {
  let s = v;
  const d = DESCRIBE.exec(v);
  if (d) {
    s = `${d[1]}+${d[2]}`;
  } else {
    const p = PSEUDO.exec(v);
    if (p) s = `${p[1]}+dev`;
  }
  return s.length > 16 ? `${s.slice(0, 15)}…` : s;
}
