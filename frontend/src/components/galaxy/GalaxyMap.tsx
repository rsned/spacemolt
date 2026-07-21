import { useMemo, useState, useRef, type WheelEvent, type MouseEvent } from 'react';
import { useGalaxyMap, type AgentLocation, type GalaxySystem } from '../../lib/useGalaxyMap';

interface GalaxyMapProps {
  systems?: GalaxySystem[];
  overlay?: (project: (x: number, y: number) => { x: number; y: number }) => React.ReactNode;
  onSystemClick?: (system: GalaxySystem) => void;
}

// ---------- Territory blob constants ----------
// SVG filter-based metaball approach: draw circles at empire system positions,
// merge them via blur + threshold filter to create organic territory blobs.
const TERRITORY_CIRCLE_RADIUS = 28; // SVG viewport units - larger for more coverage
const TERRITORY_BLUR = 20; // More blur for smoother merging
const TERRITORY_BORDER_WIDTH = 2;

const ZOOM_MIN = 0.2;  // 1x zoom (fit to screen)
const ZOOM_MAX = 1.0;  // 5x zoom
const ZOOM_STEP = 0.05;

const EMPIRE_COLORS: Record<string, string> = {
  solarian: '#FFD700',      // Solarian: Golden yellow
  voidborn: '#9932CC',      // Voidborn: Deep orchid purple
  crimson: '#DC143C',       // Crimson Fleet: Crimson red
  nebula: '#00CED1',        // Nebula Collective: Dark turquoise
  outerrim: '#2E8B57',      // Outer Rim: Sea green
  neutral: '#E5E7EB',       // Neutral: Off-white
  '': '#E5E7EB',
};

// Blood red color for Pirate Strongholds
const STRONGHOLD_COLOR = '#FF0000';

// Muted gray for systems that have never been visited (last_visited_tick === 0)
const UNEXPLORED_COLOR = '#4B5563';

export const GalaxyMap: React.FC<GalaxyMapProps> = ({ systems: propSystems, overlay, onSystemClick }) => {
  const galaxyData = useGalaxyMap();

  // Zoom state
  const [zoom, setZoom] = useState(ZOOM_MIN);

  // Pan state
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [isDragging, setIsDragging] = useState(false);
  const dragStart = useRef({ x: 0, y: 0 });
  const panStart = useRef({ x: 0, y: 0 });

  // Prefer caller-supplied systems (e.g. Overmind's own useGalaxyMap('/api/overmind')
  // fetch); only fall back to this component's internal /api/systems fetch when
  // no systems prop is given (the default App.tsx galaxy view).
  const systems = useMemo(() => {
    if (propSystems) {
      return propSystems;
    }
    return galaxyData?.systems || [];
  }, [galaxyData, propSystems]);

  const agentLocations = useMemo(() => {
    return galaxyData?.agentLocations || [];
  }, [galaxyData]);

  // Calculate bounds for auto-scaling
  const bounds = useMemo(() => {
    if (systems.length === 0) return null;

    let minX = Infinity, maxX = -Infinity;
    let minY = Infinity, maxY = -Infinity;

    systems.forEach((sys) => {
      const x = sys.position.x;
      const y = sys.position.y;
      if (x < minX) minX = x;
      if (x > maxX) maxX = x;
      if (y < minY) minY = y;
      if (y > maxY) maxY = y;
    });

    return { minX, maxX, minY, maxY };
  }, [systems]);

  // Create system lookup map
  const systemMap = useMemo(() => {
    const map = new Map<string, GalaxySystem>();
    systems.forEach((sys) => map.set(sys.id, sys));
    return map;
  }, [systems]);

  // Create agent location map by system (case-insensitive matching)
  const agentsBySystem = useMemo(() => {
    const map = new Map<string, AgentLocation[]>();
    agentLocations.forEach((agent) => {
      const agents = map.get(agent.systemId.toLowerCase()) || [];
      agents.push(agent);
      map.set(agent.systemId.toLowerCase(), agents);
    });
    return map;
  }, [agentLocations]);

  // Mouse event handlers for drag-to-pan
  const handleMouseDown = (event: MouseEvent<SVGSVGElement>) => {
    event.preventDefault();
    setIsDragging(true);
    dragStart.current = { x: event.clientX, y: event.clientY };
    panStart.current = { x: pan.x, y: pan.y };
  };

  const handleMouseMove = (event: MouseEvent<SVGSVGElement>) => {
    if (!isDragging) return;

    const dx = event.clientX - dragStart.current.x;
    const dy = event.clientY - dragStart.current.y;

    setPan({
      x: panStart.current.x + dx,
      y: panStart.current.y + dy,
    });
  };

  const handleMouseUp = () => {
    setIsDragging(false);
  };

  const handleMouseLeave = () => {
    setIsDragging(false);
  };

  // Calculate base scale to fit content
  const baseScale = useMemo(() => {
    if (!bounds) return 1;

    const padding = 50;
    const width = 900;
    const height = 800;
    const contentWidth = bounds.maxX - bounds.minX;
    const contentHeight = bounds.maxY - bounds.minY;

    return Math.min(
      (width - padding * 2) / contentWidth,
      (height - padding * 2) / contentHeight
    );
  }, [bounds]);

  // Transform coordinates with zoom and pan applied
  const transform = (x: number, y: number) => {
    if (!bounds) return { x, y };

    const width = 900;
    const height = 800;

    // Apply zoom multiplier (1x to 5x)
    const zoomMultiplier = 1 + (zoom - ZOOM_MIN) * 5; // Maps 0.2->1.0 to 1x->5x
    const scale = baseScale * zoomMultiplier;

    const centerX = (bounds.minX + bounds.maxX) / 2;
    const centerY = (bounds.minY + bounds.maxY) / 2;

    return {
      x: width / 2 + (x - centerX) * scale + pan.x,
      y: height / 2 + (y - centerY) * scale + pan.y,
    };
  };

  // Handle mouse wheel zoom
  const handleWheel = (event: WheelEvent<SVGSVGElement>) => {
    event.preventDefault();

    const delta = event.deltaY > 0 ? -ZOOM_STEP : ZOOM_STEP;
    setZoom((prevZoom) => {
      const newZoom = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, prevZoom + delta));
      return newZoom;
    });
  };

  // Handle slider change
  const handleSliderChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const value = parseFloat(event.target.value);
    setZoom(value);
  };

  // Reset zoom to fit
  const handleResetZoom = () => {
    setZoom(ZOOM_MIN);
    setPan({ x: 0, y: 0 });
  };

  // Calculate zoom level display (1x to 5x)
  const zoomLevel = Math.round((1 + (zoom - ZOOM_MIN) * 4) * 10) / 10;

  // Deduplicated undirected edge list. The systems API lists each connection
  // twice per system (e.g. adhafera.connections = ['harborlight', 'harborlight', ...]),
  // which produced duplicate React keys on the <line> elements below. Duplicate
  // keys break React's reconciliation: one sibling gets orphaned and never
  // receives updated x1/y1/x2/y2 on the next render, leaving a stale line frozen
  // at its previous zoom/pan position — the "ghosting" artifact seen while
  // zooming. Deduping here (independent of zoom/pan) guarantees unique keys.
  const edges = useMemo(() => {
    const seen = new Set<string>();
    const list: { key: string; fromId: string; toId: string }[] = [];
    for (const system of systems) {
      for (const connId of system.connections) {
        if (!systemMap.has(connId) || system.id === connId) continue;
        const [a, b] = system.id <= connId ? [system.id, connId] : [connId, system.id];
        const key = `${a}-${b}`;
        if (seen.has(key)) continue;
        seen.add(key);
        list.push({ key, fromId: a, toId: b });
      }
    }
    return list;
  }, [systems, systemMap]);

  // Group systems by empire for territory rendering (memoized on systems data)
  const empireSystemGroups = useMemo(() => {
    const groups = new Map<string, GalaxySystem[]>();
    for (const sys of systems) {
      if (sys.is_stronghold) continue;
      const empire = sys.empire.toLowerCase().trim();
      if (!empire || empire === 'neutral') continue;
      const group = groups.get(empire) || [];
      group.push(sys);
      groups.set(empire, group);
    }
    return groups;
  }, [systems]);

  return (
    <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg relative overflow-hidden" style={{ height: '900px' }}>
      <div className="absolute top-4 left-4 z-10 flex gap-4 items-center bg-spacemolt-panel/95 backdrop-blur-sm p-3 rounded-lg border border-spacemolt-border shadow-lg">
        <h2 className="font-sci-fi text-cyan-400 text-lg">◄ GALAXY MAP</h2>
        <input type="text" placeholder="Search systems..." className="bg-gray-800/80 border border-gray-700 rounded px-3 py-1.5 text-sm w-48 focus:outline-none focus:border-cyan-500 transition-colors" />
        <button className="text-cyan-400 hover:text-cyan-300 transition-colors p-1">⟳</button>
      </div>

      {/* Zoom Control Slider */}
      <div className="absolute left-4 top-1/2 -translate-y-1/2 z-10 bg-spacemolt-panel/95 backdrop-blur-sm p-3 rounded-lg border border-spacemolt-border shadow-lg">
        <div className="flex flex-col items-center gap-2">
          <span className="text-cyan-400 text-xs font-sci-fi transform -rotate-90 whitespace-nowrap mb-2 font-bold">
            ZOOM
          </span>
          <div className="h-64 flex items-center">
            <input
              type="range"
              min={ZOOM_MIN}
              max={ZOOM_MAX}
              step={ZOOM_STEP}
              value={zoom}
              onChange={handleSliderChange}
              className="h-64 w-2 appearance-none bg-gray-700 rounded-lg outline-none slider-vertical cursor-pointer"
              style={{
                WebkitAppearance: 'slider-vertical',
              }}
            />
          </div>
          <div className="text-cyan-400 text-sm font-mono font-bold">
            {zoomLevel}x
          </div>
          <button
            onClick={handleResetZoom}
            className="text-xs text-gray-400 hover:text-cyan-400 transition-colors p-1"
            title="Reset zoom"
          >
            ⟲
          </button>
        </div>
      </div>

      <div className="absolute top-4 right-4 z-10 bg-spacemolt-panel/95 backdrop-blur-sm p-4 rounded-lg border border-spacemolt-border shadow-lg text-xs space-y-2">
        <div className="font-sci-fi text-cyan-400 mb-3 text-sm font-bold">EMPIRES</div>
        {Object.entries(EMPIRE_COLORS)
          .filter(([empire]) => empire !== '') // Don't show empty string in legend
          .map(([empire, color]) => (
          <div key={empire} className="flex items-center gap-2">
            <div className="w-3 h-3 rounded-full" style={{ backgroundColor: color, boxShadow: `0 0 6px ${color}40` }} />
            <span className="text-gray-300 capitalize font-medium">{empire === 'neutral' ? 'Neutral' : empire}</span>
          </div>
        ))}
        <div className="flex items-center gap-2 mt-3 pt-3 border-t border-gray-700">
          <div className="w-3 h-3 rounded-full" style={{ backgroundColor: UNEXPLORED_COLOR }} />
          <span className="text-gray-400 font-medium">Unexplored</span>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-3 h-3 rounded-full" style={{ backgroundColor: STRONGHOLD_COLOR, boxShadow: `0 0 6px ${STRONGHOLD_COLOR}40` }} />
          <span className="text-red-400 font-medium">Pirate Stronghold</span>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-cyan-400 text-lg">●</span>
          <span className="text-gray-300">Agent Location</span>
        </div>
      </div>

      <svg
        className="w-full h-full"
        style={{ cursor: isDragging ? 'grabbing' : 'grab' }}
        viewBox="0 0 900 800"
        onWheel={handleWheel}
        onMouseDown={handleMouseDown}
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseLeave}
      >
        <defs>
          {/* Metaball goo filter: blur circles to merge, threshold to create sharp blob,
              then split into translucent fill + opaque border ring */}
          <filter
            id="territory-goo"
            x="-15%"
            y="-15%"
            width="130%"
            height="130%"
            colorInterpolationFilters="sRGB"
          >
            <feGaussianBlur in="SourceGraphic" stdDeviation={TERRITORY_BLUR} result="blur" />
            <feColorMatrix
              in="blur"
              type="matrix"
              values="1 0 0 0 0  0 1 0 0 0  0 0 1 0 0  0 0 0 30 -12"
              result="blob"
            />
            {/* Translucent fill */}
            <feComponentTransfer in="blob" result="fill">
              <feFuncA type="linear" slope={0.35} intercept={0} />
            </feComponentTransfer>
            {/* Extract border ring: erode blob, subtract from original */}
            <feMorphology in="blob" operator="erode" radius={TERRITORY_BORDER_WIDTH} result="inner" />
            <feComposite in="blob" in2="inner" operator="out" result="borderRing" />
            <feComponentTransfer in="borderRing" result="border">
              <feFuncA type="linear" slope={0.5} intercept={0} />
            </feComponentTransfer>
            {/* Combine fill + border */}
            <feMerge>
              <feMergeNode in="fill" />
              <feMergeNode in="border" />
            </feMerge>
          </filter>
        </defs>

        {/* Territory blobs — metaball circles merged by SVG filter */}
        {Array.from(empireSystemGroups.entries()).map(([empire, empireSystems]) => {
          const color = EMPIRE_COLORS[empire] || EMPIRE_COLORS['neutral'];
          return (
            <g key={`territory-${empire}`} filter="url(#territory-goo)">
              {empireSystems.map((sys) => {
                const pos = transform(sys.position.x, sys.position.y);
                return (
                  <circle
                    key={`tc-${sys.id}`}
                    cx={pos.x}
                    cy={pos.y}
                    r={TERRITORY_CIRCLE_RADIUS}
                    fill={color}
                  />
                );
              })}
            </g>
          );
        })}

        {/* Empire name labels at territory centroids */}
        {Array.from(empireSystemGroups.entries()).map(([empire, empireSystems]) => {
          const color = EMPIRE_COLORS[empire] || EMPIRE_COLORS['neutral'];
          const cx = empireSystems.reduce((s, sys) => s + sys.position.x, 0) / empireSystems.length;
          const cy = empireSystems.reduce((s, sys) => s + sys.position.y, 0) / empireSystems.length;
          const pos = transform(cx, cy);
          return (
            <text
              key={`empire-label-${empire}`}
              x={pos.x}
              y={pos.y}
              fill={color}
              fontSize="18"
              fontWeight="bold"
              textAnchor="middle"
              dominantBaseline="central"
              opacity={0.6}
              className="pointer-events-none font-sci-fi"
              style={{
                textTransform: 'uppercase',
                letterSpacing: '0.2em',
                textShadow: '0 0 10px rgba(0,0,0,0.5)'
              }}
            >
              {empire}
            </text>
          );
        })}

        {/* Connections — deduplicated edge list, see `edges` above */}
        {edges.map((edge) => {
          const fromSystem = systemMap.get(edge.fromId);
          const toSystem = systemMap.get(edge.toId);
          if (!fromSystem || !toSystem) return null;
          const from = transform(fromSystem.position.x, fromSystem.position.y);
          const to = transform(toSystem.position.x, toSystem.position.y);

          return (
            <line
              key={edge.key}
              x1={from.x}
              y1={from.y}
              x2={to.x}
              y2={to.y}
              stroke="#67e8f9"
              strokeWidth="1"
              opacity="0.6"
              strokeDasharray="none"
            />
          );
        })}

        {/* Systems */}
        {systems.map((system) => {
          const pos = transform(system.position.x, system.position.y);
          const agentsHere = agentsBySystem.get(system.id.toLowerCase()) || [];
          const hasAgents = agentsHere.length > 0;
          const isStronghold = system.is_stronghold;

          // Unexplored systems (never visited) render as muted gray regardless of empire
          // Strongholds override empire colors with blood red
          let color;
          if (system.last_visited_tick === 0) {
            color = UNEXPLORED_COLOR;
          } else if (isStronghold) {
            color = STRONGHOLD_COLOR;
          } else {
            const empire = system.empire.toLowerCase().trim() || 'neutral';
            color = EMPIRE_COLORS[empire] || EMPIRE_COLORS['neutral'];
          }

          return (
            <g key={system.id}>
              {/* Outer glow for systems with agents */}
              {hasAgents && (
                <>
                  <circle
                    cx={pos.x}
                    cy={pos.y}
                    r="18"
                    fill="none"
                    stroke="#22d3ee"
                    strokeWidth="2"
                    opacity="0.6"
                    className="animate-pulse-slow"
                  />
                  <circle
                    cx={pos.x}
                    cy={pos.y}
                    r="22"
                    fill="none"
                    stroke="#22d3ee"
                    strokeWidth="1"
                    opacity="0.3"
                    className="animate-pulse-slow"
                  />
                </>
              )}

              {/* System marker - larger with outer ring */}
              <circle
                cx={pos.x}
                cy={pos.y}
                r="6"
                fill={color}
                stroke={color}
                strokeWidth="1"
                opacity="0.9"
                onClick={() => onSystemClick?.(system)}
                className="cursor-pointer hover:scale-125 transition-transform"
                style={{ transformBox: 'fill-box', transformOrigin: 'center', cursor: onSystemClick ? 'pointer' : undefined }}
              />

              {/* System name */}
              <text
                x={pos.x}
                y={pos.y - 14}
                fill="#d1d5db"
                fontSize="11"
                fontWeight="500"
                textAnchor="middle"
                className="pointer-events-none"
                style={{ textShadow: '0 1px 2px rgba(0,0,0,0.8)' }}
              >
                {system.name}
              </text>

              {/* Agent indicator */}
              {hasAgents && (
                <>
                  <circle
                    cx={pos.x}
                    cy={pos.y + 22}
                    r="5"
                    fill="#22d3ee"
                    className="animate-pulse"
                    style={{ filter: 'drop-shadow(0 0 3px #22d3ee)' }}
                  />
                  <text
                    x={pos.x}
                    y={pos.y + 35}
                    fill="#22d3ee"
                    fontSize="9"
                    fontWeight="600"
                    textAnchor="middle"
                    className="pointer-events-none"
                    style={{ textShadow: '0 1px 2px rgba(0,0,0,0.8)' }}
                  >
                    {agentsHere.length}
                  </text>
                </>
              )}

              {/* Tooltip for agents */}
              {hasAgents && (
                <title>
                  {system.name}
                  {'\n'}
                  Agents: {agentsHere.map((a) => a.username).join(', ')}
                </title>
              )}
            </g>
          );
        })}

        {/* Caller-supplied overlay (e.g. Overmind live fleet layer) — drawn last so it's on top. */}
        {overlay?.(transform)}
      </svg>

      <div className="absolute bottom-4 left-4 bg-spacemolt-panel/95 backdrop-blur-sm p-4 rounded-lg border border-spacemolt-border shadow-lg text-xs">
        <div className="flex gap-6">
          <div>
            <span className="text-gray-400">Zoom: {zoomLevel}x</span>
            <div className="w-32 h-1.5 bg-gray-700 rounded-full mt-1">
              <div
                className="h-full bg-cyan-500 rounded-full transition-all duration-150 shadow-lg"
                style={{ width: `${((zoom - ZOOM_MIN) / (ZOOM_MAX - ZOOM_MIN)) * 100}%` }}
              />
            </div>
          </div>
          <div>
            <span className="text-gray-400">Pan:</span>
            <div className="text-cyan-400 font-mono mt-1 font-bold">
              X: {Math.round(pan.x)} Y: {Math.round(pan.y)}
            </div>
          </div>
        </div>
        <div className="mt-3 text-gray-400">
          <span className="font-medium">Systems:</span> {systems.length} | <span className="font-medium">Agents:</span> {agentLocations.length}
        </div>
        <div className="mt-2 text-gray-500 text-xs">
          Drag to pan • Scroll or use slider to zoom • Click ⟲ to reset
        </div>
      </div>
    </div>
  );
};
