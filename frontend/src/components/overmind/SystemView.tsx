import { useEffect, useMemo, useRef, useState, type MouseEvent, type WheelEvent } from 'react';
import type { GalaxySystem } from '../../lib/useGalaxyMap';
import { FLEETS, type AgentMove, type AgentState } from '../../lib/useFleetStream';
import { useSystemPois } from '../../lib/useSystemPois';
import { computeGates, seededScatter, type Gate } from './systemLayout';

const ZOOM_MIN = 1;
const ZOOM_MAX = 6;
const DRAG_THRESHOLD = 5;

/** Planet fill by KB class; anything unknown gets the neutral gray. */
const CLASS_COLORS: Record<string, string> = {
  terran: '#4ade80', arid: '#fbbf24', scorched: '#f87171', tundra: '#93c5fd',
  kuiper: '#a5f3fc', cometary: '#a5f3fc', oceanic: '#38bdf8', barren: '#9ca3af',
};

const RING_TYPES = new Set(['ice_field', 'asteroid_belt', 'asteroid', 'gas_cloud']);
const RING_COLORS: Record<string, string> = {
  ice_field: '#a5f3fc', asteroid_belt: '#d6d3d1', asteroid: '#d6d3d1', gas_cloud: '#c4b5fd',
};

function radius(p: { x: number; y: number }): number {
  return Math.hypot(p.x, p.y);
}

export function SystemView({ system, systems, agents, selectedId, onAgentClick, onClose }: {
  system: GalaxySystem;
  systems: GalaxySystem[];
  agents: AgentState[];
  moves: AgentMove[];
  selectedId: string | null;
  onAgentClick: (id: string) => void;
  onHoverAgents: (ids: string[]) => void;
  onClose: () => void;
}) {
  const { pois, error, retry } = useSystemPois(system.id);
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState({ width: 800, height: 600 });
  const [zoom, setZoom] = useState(ZOOM_MIN);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [isDragging, setIsDragging] = useState(false);
  const dragStart = useRef({ x: 0, y: 0 });
  const panStart = useRef({ x: 0, y: 0 });
  const didDrag = useRef(false);

  useEffect(() => {
    const update = () => {
      const el = containerRef.current;
      if (el) setDims({ width: el.clientWidth, height: el.clientHeight });
    };
    update();
    window.addEventListener('resize', update);
    return () => window.removeEventListener('resize', update);
  }, []);

  // Reset viewport when switching systems.
  useEffect(() => { setZoom(ZOOM_MIN); setPan({ x: 0, y: 0 }); }, [system.id]);

  const maxPoiR = useMemo(
    () => (pois ?? []).reduce((m, p) => Math.max(m, radius(p)), 0) || 1,
    [pois],
  );
  const gateRadius = maxPoiR * 1.25;
  const gates = useMemo(
    () => computeGates(system, systems, gateRadius),
    [system, systems, gateRadius],
  );

  // Fit gateRadius inside the viewport with padding; zoom multiplies.
  const baseScale = (Math.min(dims.width, dims.height) / 2) * 0.85 / gateRadius;
  const scale = baseScale * zoom;
  const T = (x: number, y: number) => ({
    x: dims.width / 2 + x * scale + pan.x,
    y: dims.height / 2 + y * scale + pan.y,
  });
  // Markers grow at half the zoom rate (same trick as SystemMap).
  const ms = 1 + (zoom - 1) * 0.5;

  const handleWheel = (e: WheelEvent<SVGSVGElement>) => {
    const factor = e.deltaY > 0 ? 1 / 1.15 : 1.15;
    setZoom((z) => Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, z * factor)));
  };
  const handleMouseDown = (e: MouseEvent<SVGSVGElement>) => {
    e.preventDefault();
    setIsDragging(true);
    didDrag.current = false;
    dragStart.current = { x: e.clientX, y: e.clientY };
    panStart.current = { ...pan };
  };
  const handleMouseMove = (e: MouseEvent<SVGSVGElement>) => {
    if (!isDragging) return;
    const dx = e.clientX - dragStart.current.x;
    const dy = e.clientY - dragStart.current.y;
    if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) didDrag.current = true;
    setPan({ x: panStart.current.x + dx, y: panStart.current.y + dy });
  };
  const handleMouseUp = () => setIsDragging(false);

  // Placeholder until Task 5: everything shows in the tray; dots come next.
  const unplaced = agents;

  const sun = (pois ?? []).find((p) => p.type === 'sun');

  return (
    <div ref={containerRef} className="absolute inset-0 flex flex-col bg-[#0a0a08]">
      {/* Header strip — absorbs the retired system info panel's content. */}
      <div className="flex items-center gap-4 px-3 py-1.5 border-b border-[#2a2618] bg-[#11100c] text-xs">
        <span className="text-[#d4a017] font-bold tracking-widest uppercase text-sm">{system.name}</span>
        <span className="text-[#8a8570] uppercase tracking-widest">{system.empire || 'neutral'}</span>
        <span className="text-[#8a8570]">police {system.police_level}</span>
        <span className="text-[#8a8570]">{system.connections.length} lanes</span>
        {system.is_stronghold && <span className="text-red-500 uppercase tracking-widest">pirate stronghold</span>}
        <span className="text-[#8a8570]">{agents.length} agents</span>
        <span className="ml-auto text-[10px] text-[#8a8570]">scroll to zoom · drag to pan · esc to exit</span>
        <button onClick={onClose} title="Back to galaxy (Esc)"
          className="px-2 py-0.5 border border-[#2a2618] rounded-sm text-[#8a8570] hover:text-[#d8d3c0] hover:border-[#8a8570]">
          ✕
        </button>
      </div>

      {error && (
        <div className="px-3 py-1 text-xs text-red-400 bg-[#11100c] border-b border-[#2a2618]">
          POI layout failed: {error}
          <button onClick={retry} className="ml-2 underline text-[#d4a017]">retry</button>
        </div>
      )}

      <div className="flex-1 min-h-0 relative">
        <svg
          className="w-full h-full"
          style={{ cursor: isDragging ? 'grabbing' : 'grab' }}
          onWheel={handleWheel}
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onMouseLeave={() => setIsDragging(false)}
        >
          {/* Orbit rings — one dashed circle per non-sun POI radius. */}
          {(pois ?? []).filter((p) => p.type !== 'sun').map((p) => {
            const o = T(0, 0);
            return (
              <circle key={`orbit-${p.id}`} cx={o.x} cy={o.y} r={radius(p) * scale}
                fill="none" stroke="#2a2618" strokeWidth={1} strokeDasharray="4,4" />
            );
          })}

          {/* Ring-scatter POIs: chunks along the whole orbit, KB style. */}
          {(pois ?? []).filter((p) => RING_TYPES.has(p.type)).map((p) => {
            const o = T(0, 0);
            const rr = radius(p) * scale;
            const color = RING_COLORS[p.type] ?? '#d6d3d1';
            const chunks = seededScatter(p.id, 90);
            return (
              <g key={`ring-${p.id}`} opacity={0.7}>
                {chunks.map((c, i) => {
                  const a = c.t * 2 * Math.PI;
                  const cr = rr + c.jr * 10 * ms;
                  const cx = o.x + Math.cos(a) * cr;
                  const cy = o.y + Math.sin(a) * cr;
                  const size = (1 + c.js * 2.5) * ms;
                  return (
                    <rect key={i} x={cx - size / 2} y={cy - size / 2} width={size} height={size}
                      fill={color} opacity={0.3 + c.js * 0.5}
                      transform={`rotate(${c.t * 360} ${cx} ${cy})`} />
                  );
                })}
              </g>
            );
          })}

          {/* Sun */}
          {sun && (() => {
            const s = T(sun.x, sun.y);
            return (
              <g key={sun.id}>
                <circle cx={s.x} cy={s.y} r={26 * ms} fill="#fbbf24" opacity={0.12} />
                <circle cx={s.x} cy={s.y} r={16 * ms} fill="#fbbf24" opacity={0.25} />
                <circle cx={s.x} cy={s.y} r={9 * ms} fill="#fef3c7" />
                <text x={s.x} y={s.y + 22 * ms} textAnchor="middle" fill="#8a8570" fontSize={10 * ms}>
                  {sun.name}
                </text>
              </g>
            );
          })()}

          {/* Planets, stations, and other point POIs */}
          {(pois ?? []).filter((p) => p.type !== 'sun' && !RING_TYPES.has(p.type)).map((p) => {
            const pos = T(p.x, p.y);
            if (p.type === 'station') {
              const r = 6 * ms;
              const hex = Array.from({ length: 6 }, (_, i) => {
                const a = (Math.PI / 3) * i - Math.PI / 6;
                return `${pos.x + r * Math.cos(a)},${pos.y + r * Math.sin(a)}`;
              }).join(' ');
              return (
                <g key={p.id}>
                  <polygon points={hex} fill="#11100c" stroke="#d8d3c0" strokeWidth={1.2} />
                  <text x={pos.x} y={pos.y - 12 * ms} textAnchor="middle" fill="#d8d3c0" fontSize={10 * ms}>
                    {p.name}
                  </text>
                </g>
              );
            }
            const fill = CLASS_COLORS[p.class] ?? '#9ca3af';
            return (
              <g key={p.id}>
                <circle cx={pos.x} cy={pos.y} r={5 * ms} fill={fill} stroke="#0a0a08" strokeWidth={1} />
                <text x={pos.x} y={pos.y - 10 * ms} textAnchor="middle" fill="#d8d3c0" fontSize={10 * ms}>
                  {p.name}
                </text>
              </g>
            );
          })}

          {/* Ring POI anchor labels (the POI's own position on its ring) */}
          {(pois ?? []).filter((p) => RING_TYPES.has(p.type)).map((p) => {
            const pos = T(p.x, p.y);
            return (
              <g key={`ring-anchor-${p.id}`}>
                <circle cx={pos.x} cy={pos.y} r={3 * ms} fill="none"
                  stroke={RING_COLORS[p.type] ?? '#d6d3d1'} strokeWidth={1} />
                <text x={pos.x} y={pos.y - 10 * ms} textAnchor="middle" fill="#8a8570" fontSize={9 * ms}>
                  {p.name}
                </text>
              </g>
            );
          })}

          {/* Jump gates — crosshair circles bearing toward their system. */}
          {gates.map((g: Gate) => {
            const pos = T(g.x, g.y);
            return (
              <g key={g.systemId} opacity={0.85}>
                <circle cx={pos.x} cy={pos.y} r={9 * ms} fill="none" stroke="#06b6d4" strokeWidth={1.2} />
                <line x1={pos.x - 12 * ms} y1={pos.y} x2={pos.x + 12 * ms} y2={pos.y} stroke="#06b6d4" strokeWidth={1} />
                <line x1={pos.x} y1={pos.y - 12 * ms} x2={pos.x} y2={pos.y + 12 * ms} stroke="#06b6d4" strokeWidth={1} />
                <text x={pos.x} y={pos.y - 16 * ms} textAnchor="middle" fill="#06b6d4" fontSize={10 * ms} className="font-mono">
                  {g.name}
                </text>
              </g>
            );
          })}
        </svg>

        {/* Unplaced tray — agents whose POI we can't match never vanish. */}
        {unplaced.length > 0 && (
          <div className="absolute bottom-0 left-0 right-0 flex items-center gap-2 px-3 py-1 bg-[#11100c]/90 border-t border-[#2a2618] text-[10px] flex-wrap">
            <span className="uppercase tracking-widest text-[#8a8570]">unplaced:</span>
            {unplaced.map((a) => (
              <button key={a.agent_id} onClick={() => onAgentClick(a.agent_id)}
                style={{ color: FLEETS[a.fleet] }}
                className={selectedId === a.agent_id ? 'underline' : ''}>
                {a.agent_id} ({a.poi || '?'})
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
