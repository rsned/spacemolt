// shortVersion compacts a `git describe` string for display. A build whose HEAD
// is not exactly on a tag stamps `<tag>-<n>-g<hash>` (e.g. v0.2.4-9-g2c1212f),
// which is ~3x the width of a plain tag and blows out the fixed-width fleet
// badge — the row then wraps and shoves the credits figure off its baseline.
// Render `v0.2.4+9` instead; callers keep the untouched full string in a title
// tooltip, and the commit hash is surfaced separately anyway.
//
// Anything not matching the describe shape is passed through, then hard-capped
// so no pathological version string can break the layout again.
//
//   v0.2.4-9-g2c1212f   -> v0.2.4+9
//   v0.2.4-123-gabcdef0 -> v0.2.4+123
//   v0.2.5              -> v0.2.5
//   dev / legacy        -> unchanged
export function shortVersion(v: string): string {
  const m = /^(.+)-(\d+)-g[0-9a-f]+$/.exec(v);
  const s = m ? `${m[1]}+${m[2]}` : v;
  return s.length > 16 ? `${s.slice(0, 15)}…` : s;
}
