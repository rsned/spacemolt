import { useRef, useEffect, useState, type WheelEvent, type MouseEvent } from 'react';
import type { POI, Player, JumpGate } from '../../types/game';
import { getPOIIcon, getPOIColor } from '../../lib/utils';

const ZOOM_MIN = 0.2;
const ZOOM_MAX = 1.0;
const ZOOM_STEP = 0.05;
const DRAG_THRESHOLD = 5;

interface SystemMapProps {
  pois: POI[];
  player: Player | null;
  jumpGates?: JumpGate[];
  policeLevel?: number;
  onTravelToPOI?: (poiId: string, poiType: string) => void;
  onJumpToSystem?: (systemId: string) => void;
}

export const SystemMap: React.FC<SystemMapProps> = ({ pois, player, jumpGates = [], policeLevel = 0, onTravelToPOI, onJumpToSystem }) => {
  // Show empty state if no player connected
  if (!player) {
    return (
      <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-8 h-full flex flex-col items-center justify-center">
        <svg className="w-20 h-20 text-gray-600 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <h3 className="text-gray-400 text-xl mb-2">No System Data</h3>
        <p className="text-gray-500 text-sm text-center">Connect to an agent to view the system map</p>
      </div>
    );
  }
  const isPoliced = policeLevel > 0;
  const isTraveling = player.traveling ?? false;
  const svgRef = useRef<SVGSVGElement>(null);
  const [dimensions, setDimensions] = useState({ width: 800, height: 500 });
  const [hoveredPOI, setHoveredPOI] = useState<string | null>(null);
  const [hoveredGate, setHoveredGate] = useState<string | null>(null);

  // Zoom and pan state
  const [zoom, setZoom] = useState(ZOOM_MIN);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [isDragging, setIsDragging] = useState(false);
  const dragStart = useRef({ x: 0, y: 0 });
  const panStart = useRef({ x: 0, y: 0 });
  const didDrag = useRef(false);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [travelTargetId, setTravelTargetId] = useState<string | null>(null);
  const [travelOriginPOI, setTravelOriginPOI] = useState<string | null>(null);
  const [clientTravelProgress, setClientTravelProgress] = useState(0);
  const animFrameRef = useRef<number>(0);

  // Clear action message and travel state when traveling ends
  useEffect(() => {
    if (!isTraveling && actionMessage) {
      setActionMessage(null);
      setTravelTargetId(null);
      setTravelOriginPOI(null);
      setClientTravelProgress(0);
    }
  }, [isTraveling, player.location.poi, player.location.systemId]);

  // Use server-provided travel destination if we don't have a click target
  useEffect(() => {
    if (isTraveling && !travelTargetId && player.travelDestination) {
      setTravelTargetId(player.travelDestination);
      setTravelOriginPOI(player.location.poi);
    }
  }, [isTraveling, player.travelDestination]);

  // Client-side travel animation using tick-based interpolation
  useEffect(() => {
    if (!isTraveling || !travelTargetId) return;

    const startTick = player.travelStartTick || player.tick;
    const arrivalTick = player.travelArrivalTick || 0;
    const serverProgress = player.travelProgress || 0;

    // If server gives us progress directly, use that
    if (serverProgress > 0) {
      setClientTravelProgress(serverProgress);
      return;
    }

    // If we have arrival tick, interpolate based on tick timing (~2s per tick)
    if (arrivalTick > startTick) {
      const totalTicks = arrivalTick - startTick;
      const TICK_DURATION_MS = 2000;
      const totalDurationMs = totalTicks * TICK_DURATION_MS;
      const startTime = Date.now();

      const animate = () => {
        const elapsed = Date.now() - startTime;
        const progress = Math.min(elapsed / totalDurationMs, 0.95); // Cap at 95% until server confirms
        setClientTravelProgress(progress);
        if (progress < 0.95) {
          animFrameRef.current = requestAnimationFrame(animate);
        }
      };
      animFrameRef.current = requestAnimationFrame(animate);
      return () => cancelAnimationFrame(animFrameRef.current);
    }

    // Fallback: simple linear animation over 5 seconds
    const startTime = Date.now();
    const animate = () => {
      const elapsed = Date.now() - startTime;
      const progress = Math.min(elapsed / 5000, 0.95);
      setClientTravelProgress(progress);
      if (progress < 0.95) {
        animFrameRef.current = requestAnimationFrame(animate);
      }
    };
    animFrameRef.current = requestAnimationFrame(animate);
    return () => cancelAnimationFrame(animFrameRef.current);
  }, [isTraveling, travelTargetId, player.travelArrivalTick, player.travelStartTick, player.travelProgress]);

  // Update dimensions on resize
  useEffect(() => {
    const updateDimensions = () => {
      if (svgRef.current) {
        const rect = svgRef.current.getBoundingClientRect();
        setDimensions({ width: rect.width, height: rect.height });
      }
    };

    updateDimensions();
    window.addEventListener('resize', updateDimensions);
    return () => window.removeEventListener('resize', updateDimensions);
  }, []);

  // Reset zoom/pan when system changes (POIs change)
  useEffect(() => {
    setZoom(ZOOM_MIN);
    setPan({ x: 0, y: 0 });
  }, [pois]);

  // Calculate base scale to fit all POIs with 20% padding
  const calculateBaseScale = () => {
    let maxDistance = 0;
    pois.forEach((poi) => {
      const distance = Math.sqrt(poi.x * poi.x + poi.y * poi.y);
      if (distance > maxDistance) {
        maxDistance = distance;
      }
    });

    if (maxDistance === 0) maxDistance = 1;

    const limitingDimension = Math.min(dimensions.width / 2, dimensions.height / 2);
    const padding = 0.8;
    return (limitingDimension * padding) / maxDistance;
  };

  const baseScale = calculateBaseScale();
  const zoomMultiplier = 1 + (zoom - ZOOM_MIN) * 5; // 1x at ZOOM_MIN, 5x at ZOOM_MAX
  const scale = baseScale * zoomMultiplier;
  const centerX = dimensions.width / 2;
  const centerY = dimensions.height / 2;

  // Marker scale factor — markers grow proportionally with zoom so they
  // maintain their visual relationship to the map distances.
  const ms = zoomMultiplier;

  // Transform game coordinates to screen coordinates with zoom and pan
  const transform = (poiX: number, poiY: number) => ({
    x: poiX * scale + centerX + pan.x,
    y: -poiY * scale + centerY + pan.y,
  });

  // Precompute screen positions for POIs
  const poiScreenPositions = pois.map((poi) => {
    const pos = transform(poi.x, poi.y);
    return { poi, x: pos.x, y: pos.y };
  });

  // Precompute screen positions for jump gates
  const maxPOIDistance = pois.length > 0
    ? Math.sqrt(Math.max(...pois.map((p) => p.x * p.x + p.y * p.y)))
    : 0;
  const gateRadius = (maxPOIDistance || 1) * 1.15;
  const gateScreenPositions = jumpGates.map((gate) => {
    const angleRad = (gate.angle - 90) * (Math.PI / 180);
    const gateX = Math.cos(angleRad) * gateRadius;
    const gateY = -Math.sin(angleRad) * gateRadius; // negate because transform already flips Y
    const pos = transform(gateX, gateY);
    return { gate, x: pos.x, y: pos.y };
  });

  // Zoom display level (1x to 5x)
  const zoomLevel = Math.round(zoomMultiplier * 10) / 10;

  // Minimum distance of any resource belt from the sun.
  // Planets closer than this get a rocky planet icon instead of a gas giant.
  const minBeltDist = pois
    .filter((p) => p.type === 'asteroid_belt' || p.type === 'asteroid' || p.type === 'ice_field')
    .reduce((min, p) => Math.min(min, Math.sqrt(p.x * p.x + p.y * p.y)), Infinity);

  // Find nearest clickable POI or gate within hit radius
  const HIT_RADIUS = 40 * ms;
  const findNearest = (mx: number, my: number): { type: 'poi'; poi: POI } | { type: 'gate'; gate: JumpGate } | null => {
    let bestDist = HIT_RADIUS;
    let best: { type: 'poi'; poi: POI } | { type: 'gate'; gate: JumpGate } | null = null;

    for (const pp of poiScreenPositions) {
      // Sun is a valid travel target too
      const isCurrent = pp.poi.id === player.location.poi || pp.poi.name === player.location.poi;
      if (isCurrent) continue;
      const dist = Math.sqrt((mx - pp.x) ** 2 + (my - pp.y) ** 2);
      if (dist < bestDist) {
        bestDist = dist;
        best = { type: 'poi', poi: pp.poi };
      }
    }

    for (const gp of gateScreenPositions) {
      const dist = Math.sqrt((mx - gp.x) ** 2 + (my - gp.y) ** 2);
      if (dist < bestDist) {
        bestDist = dist;
        best = { type: 'gate', gate: gp.gate };
      }
    }

    return best;
  };

  // Wheel zoom handler
  const handleWheel = (e: WheelEvent<SVGSVGElement>) => {
    e.preventDefault();
    const delta = e.deltaY > 0 ? -ZOOM_STEP : ZOOM_STEP;
    setZoom((prev) => Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, prev + delta)));
  };

  // Drag-to-pan handlers
  const handleMouseDown = (e: MouseEvent<SVGSVGElement>) => {
    e.preventDefault();
    setIsDragging(true);
    didDrag.current = false;
    dragStart.current = { x: e.clientX, y: e.clientY };
    panStart.current = { x: pan.x, y: pan.y };
  };

  const handleSvgMouseMove = (e: MouseEvent<SVGSVGElement>) => {
    if (isDragging) {
      const dx = e.clientX - dragStart.current.x;
      const dy = e.clientY - dragStart.current.y;
      if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) {
        didDrag.current = true;
      }
      setPan({ x: panStart.current.x + dx, y: panStart.current.y + dy });
      return;
    }

    if (isTraveling) return;
    const svg = svgRef.current;
    if (!svg) return;
    const rect = svg.getBoundingClientRect();
    const mx = e.clientX - rect.left;
    const my = e.clientY - rect.top;
    const nearest = findNearest(mx, my);
    if (nearest?.type === 'poi') {
      setHoveredPOI(nearest.poi.id);
      setHoveredGate(null);
    } else if (nearest?.type === 'gate') {
      setHoveredGate(nearest.gate.id);
      setHoveredPOI(null);
    } else {
      setHoveredPOI(null);
      setHoveredGate(null);
    }
  };

  const handleMouseUp = () => {
    setIsDragging(false);
  };

  const handleSvgClick = (e: MouseEvent<SVGSVGElement>) => {
    if (didDrag.current) return;
    if (isTraveling) return;
    const svg = svgRef.current;
    if (!svg) return;
    const rect = svg.getBoundingClientRect();
    const mx = e.clientX - rect.left;
    const my = e.clientY - rect.top;
    const nearest = findNearest(mx, my);
    if (nearest?.type === 'poi' && onTravelToPOI) {
      setTravelOriginPOI(player.location.poi);
      setTravelTargetId(nearest.poi.id);
      setClientTravelProgress(0);
      onTravelToPOI(nearest.poi.id, nearest.poi.type);
      setActionMessage(`Traveling to ${nearest.poi.name}...`);
    } else if (nearest?.type === 'gate' && onJumpToSystem) {
      setTravelOriginPOI(player.location.poi);
      setTravelTargetId(nearest.gate.id);
      setClientTravelProgress(0);
      onJumpToSystem(nearest.gate.id);
      setActionMessage(`Jumping to ${nearest.gate.name}...`);
    }
  };

  const handleSvgMouseLeave = () => {
    setIsDragging(false);
    setHoveredPOI(null);
    setHoveredGate(null);
  };

  const handleSliderChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setZoom(parseFloat(e.target.value));
  };

  const handleResetZoom = () => {
    setZoom(ZOOM_MIN);
    setPan({ x: 0, y: 0 });
  };

  return (
    <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg p-4 relative" style={{ height: '900px' }}>
      <div className="flex justify-between items-center mb-4">
        <h2 className="font-sci-fi text-cyan-400">{player.location.system} SYSTEM</h2>
        <div className="flex items-center gap-4 text-sm">
          <span className={isPoliced ? 'text-green-400' : 'text-red-400'}>
            {isPoliced ? '🛡 Policed' : '☠ Lawless'}
          </span>
          <span className="text-gray-400 font-mono">Tick: {player.tick}</span>
        </div>
      </div>

      {actionMessage && (
        <div className="absolute top-16 left-1/2 -translate-x-1/2 z-10 px-4 py-2 bg-cyan-900/80 border border-cyan-600 rounded text-cyan-300 text-sm font-mono animate-pulse">
          {actionMessage}
        </div>
      )}

      {/* Zoom Control Slider */}
      <div className="absolute left-4 top-1/2 -translate-y-1/2 z-10 bg-spacemolt-panel/95 backdrop-blur-sm p-3 rounded-lg border border-spacemolt-border shadow-lg">
        <div className="flex flex-col items-center gap-2">
          <span className="text-cyan-400 text-xs font-sci-fi transform -rotate-90 whitespace-nowrap mb-2 font-bold">
            ZOOM
          </span>
          <div className="h-48 flex items-center">
            <input
              type="range"
              min={ZOOM_MIN}
              max={ZOOM_MAX}
              step={ZOOM_STEP}
              value={zoom}
              onChange={handleSliderChange}
              className="h-48 w-2 appearance-none bg-gray-700 rounded-lg outline-none slider-vertical cursor-pointer"
              style={{ WebkitAppearance: 'slider-vertical' }}
            />
          </div>
          <div className="text-cyan-400 text-sm font-mono font-bold">
            {zoomLevel}x
          </div>
          <button
            onClick={handleResetZoom}
            className="text-xs text-gray-400 hover:text-cyan-400 transition-colors p-1"
            title="Reset zoom and pan"
          >
            &#x27F2;
          </button>
        </div>
      </div>

      <svg
        ref={svgRef}
        className="w-full h-full"
        style={{
          maxHeight: '800px',
          cursor: isDragging ? 'grabbing' : (hoveredPOI || hoveredGate) ? 'pointer' : 'grab',
        }}
        onWheel={handleWheel}
        onMouseDown={handleMouseDown}
        onMouseMove={handleSvgMouseMove}
        onMouseUp={handleMouseUp}
        onClick={handleSvgClick}
        onMouseLeave={handleSvgMouseLeave}
      >
        {/* Axis lines */}
        <g opacity="0.8">
          {/* X-axis */}
          <line x1="0" y1={centerY + pan.y} x2={dimensions.width} y2={centerY + pan.y} stroke="#9ca3af" strokeWidth="1" />
          {/* Y-axis */}
          <line x1={centerX + pan.x} y1="0" x2={centerX + pan.x} y2={dimensions.height} stroke="#9ca3af" strokeWidth="1" />

          {/* X-axis tick marks and labels */}
          {Array.from({ length: Math.ceil(dimensions.width / 2 / scale) + 5 }, (_, i) => {
            const offset = i === 0 ? 0 : i;
            const x = centerX + pan.x + (offset * scale);
            const xNeg = centerX + pan.x - (offset * scale);
            const axisY = centerY + pan.y;
            return (
              <g key={`x-tick-${i}`}>
                {x < dimensions.width && x > 0 && (
                  <>
                    <line x1={x} y1={axisY - 5} x2={x} y2={axisY + 5} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={x} y={axisY + 18} fill="#9ca3af" fontSize="9" textAnchor="middle">{offset}</text>}
                  </>
                )}
                {xNeg > 0 && xNeg < dimensions.width && (
                  <>
                    <line x1={xNeg} y1={axisY - 5} x2={xNeg} y2={axisY + 5} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={xNeg} y={axisY + 18} fill="#9ca3af" fontSize="9" textAnchor="middle">-{offset}</text>}
                  </>
                )}
              </g>
            );
          })}

          {/* Y-axis tick marks and labels */}
          {Array.from({ length: Math.ceil(dimensions.height / 2 / scale) + 5 }, (_, i) => {
            const offset = i === 0 ? 0 : i;
            const y = centerY + pan.y - (offset * scale);
            const yNeg = centerY + pan.y + (offset * scale);
            const axisX = centerX + pan.x;
            return (
              <g key={`y-tick-${i}`}>
                {y > 0 && y < dimensions.height && (
                  <>
                    <line x1={axisX - 5} y1={y} x2={axisX + 5} y2={y} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={axisX - 10} y={y + 3} fill="#9ca3af" fontSize="9" textAnchor="end">{offset}</text>}
                  </>
                )}
                {yNeg < dimensions.height && yNeg > 0 && (
                  <>
                    <line x1={axisX - 5} y1={yNeg} x2={axisX + 5} y2={yNeg} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={axisX - 10} y={yNeg + 3} fill="#9ca3af" fontSize="9" textAnchor="end">-{offset}</text>}
                  </>
                )}
              </g>
            );
          })}
        </g>

        {/* Orbital paths for planets and POIs */}
        {pois
          .filter((poi) => poi.type !== 'sun')
          .map((poi) => {
            const radius = Math.sqrt(poi.x * poi.x + poi.y * poi.y);
            const orbitRadius = radius * scale;
            const origin = transform(0, 0);

            return (
              <circle
                key={`orbit-${poi.id}`}
                cx={origin.x}
                cy={origin.y}
                r={orbitRadius}
                fill="none"
                stroke="#9ca3af"
                strokeWidth="1"
                strokeDasharray="4,4"
                opacity="0.8"
              />
            );
          })}

        {pois.map((poi) => {
          const { x, y } = transform(poi.x, poi.y);
          const isCurrent = poi.id === player.location.poi || poi.name === player.location.poi;
          const isHovered = hoveredPOI === poi.id;

          return (
            <g key={poi.id}>
              {/* Sun glow effect */}
              {poi.type === 'sun' && (
                <>
                  <circle
                    cx={x}
                    cy={y}
                    r={17 * ms}
                    fill="none"
                    stroke="#fbbf24"
                    strokeWidth="1"
                    opacity="0.3"
                    className="animate-pulse-slow"
                  />
                  <circle
                    cx={x}
                    cy={y}
                    r={20 * ms}
                    fill="#fbbf24"
                    opacity="0.5"
                  />
                </>
              )}

              {/* Highlight for current location */}
              {isCurrent && (
                <circle
                  cx={x}
                  cy={y}
                  r={35 * ms}
                  fill="none"
                  stroke="#22d3ee"
                  strokeWidth="2"
                  strokeDasharray="8,4"
                  className="animate-pulse-slow"
                />
              )}

              {/* Ice field: Show dashed ring with ice chunks */}
              {poi.type === 'ice_field' && (
                <g>
                  <circle
                    cx={x}
                    cy={y}
                    r={35 * ms}
                    fill="none"
                    stroke="#67e8f9"
                    strokeWidth="1.5"
                    strokeDasharray="6,3"
                    opacity="0.6"
                  />
                  {[...Array(12)].map((_, i) => {
                    const angle = (i / 12) * Math.PI * 2 + (poi.id.charCodeAt(0) * 0.5);
                    const radius = (15 + ((i * 7 + poi.id.charCodeAt(1)) % 25)) * ms;
                    const chunkX = x + Math.cos(angle) * radius;
                    const chunkY = y + Math.sin(angle) * radius;
                    const size = (3 + ((i * 3 + poi.id.charCodeAt(0)) % 4)) * ms;
                    const opacity = 0.4 + ((i * 5) % 3) * 0.1;
                    return (
                      <polygon
                        key={i}
                        points={`${chunkX},${chunkY - size} ${chunkX + size * 0.8},${chunkY} ${chunkX},${chunkY + size} ${chunkX - size * 0.8},${chunkY}`}
                        fill="#a5f3fc"
                        opacity={opacity}
                      />
                    );
                  })}
                </g>
              )}

              {/* Asteroid belt: Show dashed ring with rocky chunks */}
              {(poi.type === 'asteroid_belt' || poi.type === 'asteroid') && (
                <g>
                  <circle
                    cx={x}
                    cy={y}
                    r={35 * ms}
                    fill="none"
                    stroke="#d97706"
                    strokeWidth="1.5"
                    strokeDasharray="5,4"
                    opacity="0.5"
                  />
                  {[...Array(10)].map((_, i) => {
                    const angle = (i / 10) * Math.PI * 2 + (poi.id.charCodeAt(0) * 0.3);
                    const radius = (12 + ((i * 11 + poi.id.charCodeAt(1)) % 22)) * ms;
                    const chunkX = x + Math.cos(angle) * radius;
                    const chunkY = y + Math.sin(angle) * radius;
                    const size = (2 + ((i * 3 + poi.id.charCodeAt(0)) % 4)) * ms;
                    const opacity = 0.35 + ((i * 7) % 4) * 0.1;
                    return (
                      <polygon
                        key={i}
                        points={`${chunkX},${chunkY - size} ${chunkX + size},${chunkY + size * 0.3} ${chunkX - size * 0.6},${chunkY + size}`}
                        fill="#f59e0b"
                        opacity={opacity}
                      />
                    );
                  })}
                </g>
              )}

              {/* Gas cloud: Show dashed ring with gas blobs */}
              {poi.type === 'gas_cloud' && (
                <g>
                  <circle
                    cx={x}
                    cy={y}
                    r={35 * ms}
                    fill="none"
                    stroke="#a78bfa"
                    strokeWidth="1.5"
                    strokeDasharray="6,3"
                    opacity="0.6"
                  />
                  {[...Array(10)].map((_, i) => {
                    const angle = (i / 10) * Math.PI * 2 + (poi.id.charCodeAt(0) * 0.4);
                    const radius = (10 + ((i * 9 + poi.id.charCodeAt(1)) % 22)) * ms;
                    const blobX = x + Math.cos(angle) * radius;
                    const blobY = y + Math.sin(angle) * radius;
                    const r = (2 + ((i * 3 + poi.id.charCodeAt(0)) % 3)) * ms;
                    const opacity = 0.3 + ((i * 5) % 4) * 0.1;
                    return (
                      <circle
                        key={i}
                        cx={blobX}
                        cy={blobY}
                        r={r}
                        fill="#c4b5fd"
                        opacity={opacity}
                      />
                    );
                  })}
                </g>
              )}

              {/* Station: hexagonal outline */}
              {poi.type === 'station' && (() => {
                const r = 14 * ms;
                const hex = Array.from({ length: 6 }, (_, i) => {
                  const a = (Math.PI / 3) * i - Math.PI / 6;
                  return `${x + r * Math.cos(a)},${y + r * Math.sin(a)}`;
                }).join(' ');
                return (
                  <g>
                    <polygon points={hex} fill="none" stroke="#e5e7eb" strokeWidth="1.5" opacity="0.6" />
                    <polygon points={hex} fill="#374151" opacity="0.3" />
                  </g>
                );
              })()}

              {/* Hover highlight */}
              {isHovered && (
                <circle
                  cx={x}
                  cy={y}
                  r={30 * ms}
                  fill="none"
                  stroke="#22d3ee"
                  strokeWidth="2"
                  opacity="0.6"
                />
              )}

              {/* POI icon */}
              {poi.type === 'planet' && poi.id !== 'sol_earth' && Math.sqrt(poi.x * poi.x + poi.y * poi.y) < minBeltDist ? (
                // Inner planet — banded sphere with wavy horizontal stripes and glossy highlight
                (() => {
                  const r = 14 * ms;
                  const id = `planet-${poi.id}`;
                  // Derive stable hue from POI id so each planet is unique
                  const seed = poi.id.split('').reduce((a, c) => a + c.charCodeAt(0), 0);
                  const baseHue = 180 + (seed % 60); // range of ocean/teal/blue tones
                  const light = `hsl(${baseHue}, 50%, 55%)`;
                  const mid = `hsl(${baseHue}, 55%, 42%)`;
                  const dark = `hsl(${baseHue + 10}, 60%, 30%)`;
                  // Wavy band y-offsets (fraction of radius, -1 to 1)
                  const bands = [-0.7, -0.35, -0.05, 0.25, 0.55, 0.8];
                  // Seed-based wave variation per band
                  const wave = (i: number, t: number) => {
                    const amp = r * (0.08 + ((seed * (i + 1) * 3) % 7) * 0.015);
                    const freq = 2.5 + ((seed * (i + 2)) % 5) * 0.3;
                    const phase = ((seed * (i + 1) * 7) % 100) * 0.1;
                    return Math.sin(t * freq + phase) * amp;
                  };
                  return (
                    <g style={{ filter: isHovered ? 'brightness(1.3)' : undefined }}>
                      <defs>
                        <clipPath id={`${id}-clip`}>
                          <circle cx={x} cy={y} r={r} />
                        </clipPath>
                        <radialGradient id={`${id}-gloss`} cx="35%" cy="30%" r="65%">
                          <stop offset="0%" stopColor="white" stopOpacity="0.35" />
                          <stop offset="60%" stopColor="white" stopOpacity="0" />
                        </radialGradient>
                        <radialGradient id={`${id}-shadow`} cx="65%" cy="70%" r="60%">
                          <stop offset="0%" stopColor="black" stopOpacity="0" />
                          <stop offset="100%" stopColor="black" stopOpacity="0.3" />
                        </radialGradient>
                      </defs>
                      {/* Base sphere */}
                      <circle cx={x} cy={y} r={r} fill={mid} />
                      {/* Wavy bands clipped to sphere */}
                      <g clipPath={`url(#${id}-clip)`}>
                        {bands.map((bandY, i) => {
                          const cy0 = y + bandY * r;
                          const bandH = r * (0.12 + ((seed * (i + 3)) % 5) * 0.02);
                          // Build a wavy path across the planet width
                          const steps = 12;
                          const left = x - r * 1.1;
                          const right = x + r * 1.1;
                          const dx = (right - left) / steps;
                          let d = `M ${left} ${cy0 + wave(i, 0)}`;
                          for (let s = 1; s <= steps; s++) {
                            const px = left + dx * s;
                            const py = cy0 + wave(i, s / steps * Math.PI * 2);
                            const cpx = px - dx / 2;
                            const cpy = cy0 + wave(i, (s - 0.5) / steps * Math.PI * 2);
                            d += ` Q ${cpx} ${cpy} ${px} ${py}`;
                          }
                          // Close the band by going back along bottom edge
                          const bottomY = cy0 + bandH;
                          d += ` L ${right} ${bottomY + wave(i, Math.PI * 2)}`;
                          for (let s = steps - 1; s >= 0; s--) {
                            const px = left + dx * s;
                            const py = bottomY + wave(i, s / steps * Math.PI * 2) * 0.7;
                            const cpx = px + dx / 2;
                            const cpy = bottomY + wave(i, (s + 0.5) / steps * Math.PI * 2) * 0.7;
                            d += ` Q ${cpx} ${cpy} ${px} ${py}`;
                          }
                          d += ' Z';
                          return (
                            <path
                              key={i}
                              d={d}
                              fill={i % 2 === 0 ? dark : light}
                              opacity={0.7 + ((i * 3 + seed) % 3) * 0.1}
                            />
                          );
                        })}
                      </g>
                      {/* Glossy highlight (upper-left) */}
                      <circle cx={x} cy={y} r={r} fill={`url(#${id}-gloss)`} />
                      {/* Shadow (lower-right) */}
                      <circle cx={x} cy={y} r={r} fill={`url(#${id}-shadow)`} />
                      {/* Rim */}
                      <circle cx={x} cy={y} r={r} fill="none" stroke={`hsl(${baseHue}, 45%, 25%)`} strokeWidth={0.8 * ms} />
                    </g>
                  );
                })()
              ) : (
                <text
                  x={x}
                  y={y}
                  textAnchor="middle"
                  dominantBaseline="central"
                  className={getPOIColor(poi.type)}
                  style={{
                    fontSize: `${(poi.type === 'sun' ? 27 : poi.type === 'station' ? 21 : 28) * ms}px`,
                    filter: isHovered ? 'brightness(1.4)' : undefined,
                  }}
                >
                  {poi.id === 'sol_earth' ? '🌍' : getPOIIcon(poi.type)}
                </text>
              )}

              {/* POI label */}
              {poi.type !== 'sun' && (() => {
                const isResourcePOI = poi.type === 'asteroid_belt' || poi.type === 'asteroid' || poi.type === 'ice_field' || poi.type === 'gas_cloud';
                const labelY = isResourcePOI ? y - 45 * ms : y - 25 * ms;
                return (
                  <text x={x} y={labelY} fill="#e5e7eb" fontSize={11 * ms} textAnchor="middle" fontWeight="bold">
                    {poi.name}
                  </text>
                );
              })()}

              {/* Resources display */}
              {poi.resources && (
                <g>
                  {poi.resources.map((res, idx) => (
                    <text key={idx} x={x + 40 * ms} y={y - 10 * ms + idx * 14 * ms} fill="#d1d5db" fontSize={9 * ms}>
                      {res.name} {res.amount}
                    </text>
                  ))}
                </g>
              )}
            </g>
          );
        })}

        {/* Jump Gates */}
        {gateScreenPositions.map(({ gate, x: gateX, y: gateY }) => {
          const isGateHovered = hoveredGate === gate.id;

          return (
            <g key={gate.id}>
              {/* Jump gate icon */}
              <circle
                cx={gateX}
                cy={gateY}
                r={25 * ms}
                fill="none"
                stroke={isGateHovered ? '#22d3ee' : '#06b6d4'}
                strokeWidth={isGateHovered ? 3 : 2}
                opacity={isGateHovered ? 1 : 0.7}
              />
              <circle
                cx={gateX}
                cy={gateY}
                r={20 * ms}
                fill="none"
                stroke="#06b6d4"
                strokeWidth="1.5"
                opacity="0.5"
                strokeDasharray="4,2"
              />
              {/* Jump gate crosshair */}
              <circle cx={gateX} cy={gateY} r={8 * ms} fill="none" stroke="#22d3ee" strokeWidth="1.5" opacity="0.9" />
              <line x1={gateX - 12 * ms} y1={gateY} x2={gateX + 12 * ms} y2={gateY} stroke="#22d3ee" strokeWidth="1.5" opacity="0.9" />
              <line x1={gateX} y1={gateY - 12 * ms} x2={gateX} y2={gateY + 12 * ms} stroke="#22d3ee" strokeWidth="1.5" opacity="0.9" />
              {/* System name */}
              <text
                x={gateX}
                y={gateY - 35 * ms}
                fill="#06b6d4"
                fontSize={10 * ms}
                textAnchor="middle"
                className="font-mono"
              >
                {gate.name}
              </text>
              {/* System ID */}
              <text
                x={gateX}
                y={gateY + 40 * ms}
                fill="#4b5563"
                fontSize={8 * ms}
                textAnchor="middle"
                className="font-mono"
              >
                {gate.id}
              </text>
            </g>
          );
        })}

        {/* Travel line and animated ship */}
        {(() => {
          const originId = travelOriginPOI || player.location.poi;
          const originPOI = pois.find((p) => p.id === originId || p.name === originId);
          if (!originPOI) return null;

          const originPos = transform(originPOI.x, originPOI.y);
          const ox = originPos.x;
          const oy = originPos.y;

          // Find target position (POI or jump gate)
          let tx: number | null = null;
          let ty: number | null = null;
          if (isTraveling && travelTargetId) {
            const targetPOI = pois.find((p) => p.id === travelTargetId || p.name === travelTargetId);
            if (targetPOI) {
              const targetPos = transform(targetPOI.x, targetPOI.y);
              tx = targetPos.x;
              ty = targetPos.y;
            } else {
              // Check if target is a jump gate
              const targetGate = gateScreenPositions.find((gp) => gp.gate.id === travelTargetId);
              if (targetGate) {
                tx = targetGate.x;
                ty = targetGate.y;
              }
            }
          }

          // Calculate ship position
          let sx: number;
          let sy: number;
          if (isTraveling && tx !== null && ty !== null && clientTravelProgress > 0) {
            // Interpolate ship position along the travel path
            sx = ox + (tx - ox) * clientTravelProgress;
            sy = oy + (ty - oy) * clientTravelProgress;
          } else {
            // Ship at current POI
            sx = ox + 10 * ms;
            sy = oy + 10 * ms;
          }

          return (
            <g>
              {/* Dashed travel line from origin to destination */}
              {isTraveling && tx !== null && ty !== null && (
                <>
                  <line
                    x1={ox}
                    y1={oy}
                    x2={tx}
                    y2={ty}
                    stroke="#22d3ee"
                    strokeWidth="1.5"
                    strokeDasharray="6,4"
                    opacity="0.5"
                  />
                  {/* Destination marker pulse */}
                  <circle
                    cx={tx}
                    cy={ty}
                    r={20 * ms}
                    fill="none"
                    stroke="#22d3ee"
                    strokeWidth="1.5"
                    strokeDasharray="4,3"
                    opacity="0.4"
                    className="animate-pulse-slow"
                  />
                </>
              )}
              {/* Ship icon */}
              <text
                x={sx}
                y={sy}
                textAnchor="middle"
                dominantBaseline="central"
                fontSize={16 * ms}
              >
                🚀
              </text>
            </g>
          );
        })()}
      </svg>

      <div className="absolute bottom-4 left-4 right-4 flex justify-between items-end text-xs">
        <div className="bg-spacemolt-panel p-2 rounded border border-spacemolt-border">
          <span className="text-gray-400">Connected Systems:</span>
          {jumpGates.length > 0 ? jumpGates.map((gate) => (
            <button
              key={gate.id}
              disabled={isTraveling || !onJumpToSystem}
              onClick={() => { onJumpToSystem?.(gate.id); setActionMessage(`Jumping to ${gate.name}...`); }}
              className={`ml-2 px-2 py-1 rounded ${
                isTraveling || !onJumpToSystem
                  ? 'bg-gray-800 text-gray-600 cursor-not-allowed'
                  : 'bg-cyan-900/50 hover:bg-cyan-800 text-cyan-300'
              }`}
            >
              {gate.name}
            </button>
          )) : (
            <span className="ml-2 text-gray-600">None</span>
          )}
        </div>
        {isTraveling && (
          <div className="bg-yellow-900/50 p-2 rounded border border-yellow-700 text-yellow-400 animate-pulse">
            Traveling...
          </div>
        )}
      </div>
    </div>
  );
};
