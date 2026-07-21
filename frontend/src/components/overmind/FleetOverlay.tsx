import { useEffect, useMemo, useRef, useState } from 'react';
import type { GalaxySystem } from '../../lib/useGalaxyMap';
import { FLEETS, type AgentMove, type AgentState } from '../../lib/useFleetStream';

const MOVE_MS = 2000;

interface Anim { fromX: number; fromY: number; started: number }

/** Deterministic orbit offset so co-located agents fan out, stable per agent. */
function orbit(_agentId: string, index: number, count: number): { dx: number; dy: number } {
  const angle = (2 * Math.PI * index) / Math.max(count, 1);
  const r = count > 1 ? 8 : 0;
  return { dx: r * Math.cos(angle), dy: r * Math.sin(angle) };
}

/** Fleet with the most agents present at a system; ties keep the first-seen fleet (stable). */
function dominantFleet(list: AgentState[]): string {
  const counts = new Map<string, number>();
  for (const a of list) counts.set(a.fleet, (counts.get(a.fleet) ?? 0) + 1);
  let best = list[0].fleet, bestCount = 0;
  for (const [fleet, count] of counts) {
    if (count > bestCount) { best = fleet; bestCount = count; }
  }
  return best;
}

export function FleetOverlay({ agents, moves, systems, project, visibleFleets, selectedId, onAgentClick }: {
  agents: AgentState[];
  moves: AgentMove[];
  systems: GalaxySystem[];
  project: (x: number, y: number) => { x: number; y: number };
  visibleFleets: Set<string>;
  selectedId: string | null;
  onAgentClick: (id: string) => void;
}) {
  const sysById = useMemo(() => new Map(systems.map((s) => [s.id, s])), [systems]);
  const anims = useRef(new Map<string, Anim>());
  const [, force] = useState(0);

  // Register animations for fresh moves; tick a re-render until they expire.
  useEffect(() => {
    const now = performance.now();
    for (const m of moves) {
      const from = sysById.get(m.from_system_id);
      if (from) {
        anims.current.set(m.agent.agent_id, {
          fromX: from.position.x, fromY: from.position.y, started: now,
        });
      }
    }
    if (anims.current.size === 0) return;
    const iv = setInterval(() => {
      const t = performance.now();
      for (const [id, a] of anims.current) {
        if (t - a.started > MOVE_MS) anims.current.delete(id);
      }
      force((n) => n + 1);
      if (anims.current.size === 0) clearInterval(iv);
    }, 50);
    return () => clearInterval(iv);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [moves]);

  // Group visible agents by system for badges + orbit fanning.
  const bySystem = useMemo(() => {
    const map = new Map<string, AgentState[]>();
    for (const a of agents) {
      if (!visibleFleets.has(a.fleet)) continue;
      const list = map.get(a.system_id) ?? [];
      list.push(a);
      map.set(a.system_id, list);
    }
    return map;
  }, [agents, visibleFleets]);

  const now = performance.now();
  return (
    <g>
      {[...bySystem.entries()].map(([sysId, list]) => {
        const sys = sysById.get(sysId);
        if (!sys) return null;
        const center = project(sys.position.x, sys.position.y);
        const dominantColor = FLEETS[dominantFleet(list)] ?? '#d4a017';
        return (
          <g key={sysId}>
            {/* soft glow tinted by the dominant fleet present */}
            <circle cx={center.x} cy={center.y} r={14} fill={dominantColor} opacity={0.15} />
            {/* count badge */}
            <g transform={`translate(${center.x + 8}, ${center.y - 10})`}>
              <rect x={-2} y={-8} width={list.length >= 10 ? 18 : 12} height={11}
                rx={2} fill="#11100c" stroke={dominantColor} strokeWidth={0.5} />
              <text x={list.length >= 10 ? 7 : 4} y={1} textAnchor="middle"
                fontSize={8} fill={dominantColor} fontFamily="monospace">{list.length}</text>
            </g>
            {list.map((a, i) => {
              const { dx, dy } = orbit(a.agent_id, i, list.length);
              let x = center.x + dx, y = center.y + dy;
              const anim = anims.current.get(a.agent_id);
              const t = anim ? Math.min(1, (now - anim.started) / MOVE_MS) : 0;
              if (anim) {
                const from = project(anim.fromX, anim.fromY);
                x = from.x + (center.x + dx - from.x) * t;
                y = from.y + (center.y + dy - from.y) * t;
              }
              const color = FLEETS[a.fleet] ?? '#fff';
              const selected = selectedId === a.agent_id;
              return (
                <g key={a.agent_id} onClick={() => onAgentClick(a.agent_id)} style={{ cursor: 'pointer' }}>
                  {anim && (
                    <>
                      {/* lane-brighten overlay: full from->to segment lights up in the mover's
                          fleet color for the duration of the move, fading as it completes */}
                      <line x1={project(anim.fromX, anim.fromY).x} y1={project(anim.fromX, anim.fromY).y}
                        x2={center.x} y2={center.y} stroke={color} strokeWidth={2.5} opacity={0.3 * (1 - t)} />
                      <line x1={project(anim.fromX, anim.fromY).x} y1={project(anim.fromX, anim.fromY).y}
                        x2={x} y2={y} stroke={color} strokeWidth={0.5} opacity={0.4} />
                    </>
                  )}
                  {selected && <circle cx={x} cy={y} r={6} fill="none" stroke="#fff" strokeWidth={0.8} />}
                  {a.docked
                    ? <circle cx={x} cy={y} r={3} fill="none" stroke={color} strokeWidth={1.2} />
                    : <circle cx={x} cy={y} r={3} fill={color} />}
                  <title>{a.agent_id} · {a.system_name}/{a.poi} · ₡{Math.round(a.credits)}</title>
                </g>
              );
            })}
          </g>
        );
      })}
    </g>
  );
}
