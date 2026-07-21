import type { GalaxySystem } from '../../lib/useGalaxyMap';

/** A jump gate placed on the system periphery, in POI-space coordinates. */
export interface Gate {
  systemId: string;
  name: string;
  x: number;
  y: number;
}

/** Gates sit at gateRadius from the sun, bearing toward the connected system
 * in galaxy coordinates — matching KB page placement (e.g. the Sol gate sits
 * on the Sol-facing edge of Alpha Centauri). */
export function computeGates(current: GalaxySystem, all: GalaxySystem[], gateRadius: number): Gate[] {
  const byId = new Map(all.map((s) => [s.id, s]));
  const gates: Gate[] = [];
  for (const connId of new Set(current.connections)) {
    const target = byId.get(connId);
    if (!target) continue;
    const dx = target.position.x - current.position.x;
    const dy = target.position.y - current.position.y;
    const len = Math.hypot(dx, dy) || 1;
    gates.push({
      systemId: target.id,
      name: target.name,
      x: (dx / len) * gateRadius,
      y: (dy / len) * gateRadius,
    });
  }
  return gates;
}

/** Deterministic fan placement so co-located agent dots stay individually
 * hoverable; mirrors FleetOverlay's orbit() but with a larger radius. */
export function fanOffset(index: number, count: number, r: number): { dx: number; dy: number } {
  if (count <= 1) return { dx: r, dy: r };
  const angle = (2 * Math.PI * index) / count;
  return { dx: r * Math.cos(angle), dy: r * Math.sin(angle) };
}

/** Deterministic per-seed scatter parameters for belt/ice/gas ring chunks:
 * t = angle fraction around the ring, jr = radial jitter [-1,1],
 * js = size jitter [0,1). Stable across renders (no Math.random). */
export function seededScatter(seed: string, count: number): { t: number; jr: number; js: number }[] {
  let h = 2166136261;
  for (const c of seed) {
    h = Math.imul(h ^ c.charCodeAt(0), 16777619);
  }
  const out: { t: number; jr: number; js: number }[] = [];
  for (let i = 0; i < count; i++) {
    h = Math.imul(h ^ (i + 1), 2654435761);
    const a = ((h >>> 8) & 0xffff) / 0x10000;
    h = Math.imul(h ^ 0x9e3779b9, 2246822519);
    const b = ((h >>> 8) & 0xffff) / 0x10000;
    h = Math.imul(h ^ 0x85ebca6b, 3266489917);
    const c = ((h >>> 8) & 0xffff) / 0x10000;
    out.push({ t: (i + a) / count, jr: b * 2 - 1, js: c });
  }
  return out;
}
