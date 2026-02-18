import { useRef, useEffect, useState } from 'react';
import type { POI, Player, JumpGate } from '../../types/game';
import { getPOIIcon, getPOIColor } from '../../lib/utils';

interface SystemMapProps {
  pois: POI[];
  player: Player;
  jumpGates?: JumpGate[];
  policeLevel?: number;
  onTravelToPOI?: (poiId: string) => void;
  onJumpToSystem?: (systemId: string) => void;
}

export const SystemMap: React.FC<SystemMapProps> = ({ pois, player, jumpGates = [], policeLevel = 0, onTravelToPOI, onJumpToSystem }) => {
  const isPoliced = policeLevel > 0;
  const isTraveling = player.traveling ?? false;
  const svgRef = useRef<SVGSVGElement>(null);
  const [dimensions, setDimensions] = useState({ width: 800, height: 500 });
  const [hoveredPOI, setHoveredPOI] = useState<string | null>(null);
  const [hoveredGate, setHoveredGate] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);

  // Clear action message when traveling state ends or system/poi changes
  useEffect(() => {
    if (!isTraveling && actionMessage) {
      setActionMessage(null);
    }
  }, [isTraveling, player.location.poi, player.location.systemId]);

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

  // Calculate scale to fit all POIs with 20% padding
  const calculateScale = () => {
    // Find the POI farthest from origin (0, 0)
    let maxDistance = 0;
    pois.forEach((poi) => {
      const distance = Math.sqrt(poi.x * poi.x + poi.y * poi.y);
      if (distance > maxDistance) {
        maxDistance = distance;
      }
    });

    // If no POIs or all at origin, use default
    if (maxDistance === 0) maxDistance = 1;

    // Use the limiting dimension (min of half-width and half-height)
    const limitingDimension = Math.min(dimensions.width / 2, dimensions.height / 2);

    // Apply 20% padding (use 80% of the limiting dimension)
    const padding = 0.8;
    const scale = (limitingDimension * padding) / maxDistance;

    return scale;
  };

  const scale = calculateScale();
  const centerX = dimensions.width / 2;
  const centerY = dimensions.height / 2;

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

      <svg
        ref={svgRef}
        className="w-full h-full"
        style={{ maxHeight: '800px' }}
      >
        {/* Axis lines */}
        <g opacity="0.8">
          {/* X-axis */}
          <line x1="0" y1={centerY} x2={dimensions.width} y2={centerY} stroke="#9ca3af" strokeWidth="1" />
          {/* Y-axis */}
          <line x1={centerX} y1="0" x2={centerX} y2={dimensions.height} stroke="#9ca3af" strokeWidth="1" />

          {/* X-axis tick marks and labels */}
          {Array.from({ length: Math.ceil(dimensions.width / 2 / scale) }, (_, i) => {
            const offset = i === 0 ? 0 : i;
            const x = centerX + (offset * scale);
            const xNeg = centerX - (offset * scale);
            return (
              <g key={`x-tick-${i}`}>
                {/* Positive ticks */}
                {x < dimensions.width && (
                  <>
                    <line x1={x} y1={centerY - 5} x2={x} y2={centerY + 5} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={x} y={centerY + 18} fill="#9ca3af" fontSize="9" textAnchor="middle">{offset}</text>}
                  </>
                )}
                {/* Negative ticks */}
                {xNeg > 0 && (
                  <>
                    <line x1={xNeg} y1={centerY - 5} x2={xNeg} y2={centerY + 5} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={xNeg} y={centerY + 18} fill="#9ca3af" fontSize="9" textAnchor="middle">-{offset}</text>}
                  </>
                )}
              </g>
            );
          })}

          {/* Y-axis tick marks and labels */}
          {Array.from({ length: Math.ceil(dimensions.height / 2 / scale) }, (_, i) => {
            const offset = i === 0 ? 0 : i;
            const y = centerY - (offset * scale);
            const yNeg = centerY + (offset * scale);
            return (
              <g key={`y-tick-${i}`}>
                {/* Positive ticks (up in game coordinates, which is down in screen coordinates flipped) */}
                {y > 0 && (
                  <>
                    <line x1={centerX - 5} y1={y} x2={centerX + 5} y2={y} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={centerX - 10} y={y + 3} fill="#9ca3af" fontSize="9" textAnchor="end">{offset}</text>}
                  </>
                )}
                {/* Negative ticks */}
                {yNeg < dimensions.height && (
                  <>
                    <line x1={centerX - 5} y1={yNeg} x2={centerX + 5} y2={yNeg} stroke="#9ca3af" strokeWidth="1" />
                    {offset > 0 && <text x={centerX - 10} y={yNeg + 3} fill="#9ca3af" fontSize="9" textAnchor="end">-{offset}</text>}
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

            return (
              <circle
                key={`orbit-${poi.id}`}
                cx={centerX}
                cy={centerY}
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
          // Center on (0,0) and flip Y axis
          const x = poi.x * scale + centerX;
          const y = -poi.y * scale + centerY;
          const isCurrent = poi.id === player.location.poi || poi.name === player.location.poi;
          const isClickable = poi.type !== 'sun' && !isCurrent && !isTraveling && !!onTravelToPOI;
          const isHovered = hoveredPOI === poi.id;

          return (
            <g
              key={poi.id}
              style={{ cursor: isClickable ? 'pointer' : 'default' }}
              onClick={() => { if (isClickable) { onTravelToPOI(poi.id); setActionMessage(`Traveling to ${poi.name}...`); } }}
              onMouseEnter={() => { if (isClickable) setHoveredPOI(poi.id); }}
              onMouseLeave={() => setHoveredPOI(null)}
            >
              {/* Sun glow effect */}
              {poi.type === 'sun' && (
                <>
                  <circle
                    cx={x}
                    cy={y}
                    r="25"
                    fill="none"
                    stroke="#fbbf24"
                    strokeWidth="1"
                    opacity="0.3"
                    className="animate-pulse-slow"
                  />
                  <circle
                    cx={x}
                    cy={y}
                    r="30"
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
                  r="35"
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
                    r="35"
                    fill="none"
                    stroke="#67e8f9"
                    strokeWidth="1.5"
                    strokeDasharray="6,3"
                    opacity="0.6"
                  />
                  {[...Array(12)].map((_, i) => {
                    const angle = (i / 12) * Math.PI * 2 + (poi.id.charCodeAt(0) * 0.5);
                    const radius = 15 + ((i * 7 + poi.id.charCodeAt(1)) % 25);
                    const chunkX = x + Math.cos(angle) * radius;
                    const chunkY = y + Math.sin(angle) * radius;
                    const size = 3 + ((i * 3 + poi.id.charCodeAt(0)) % 4);
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
                    r="35"
                    fill="none"
                    stroke="#d97706"
                    strokeWidth="1.5"
                    strokeDasharray="5,4"
                    opacity="0.5"
                  />
                  {[...Array(10)].map((_, i) => {
                    const angle = (i / 10) * Math.PI * 2 + (poi.id.charCodeAt(0) * 0.3);
                    const radius = 12 + ((i * 11 + poi.id.charCodeAt(1)) % 22);
                    const chunkX = x + Math.cos(angle) * radius;
                    const chunkY = y + Math.sin(angle) * radius;
                    const size = 2 + ((i * 3 + poi.id.charCodeAt(0)) % 4);
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
                    r="35"
                    fill="none"
                    stroke="#a78bfa"
                    strokeWidth="1.5"
                    strokeDasharray="6,3"
                    opacity="0.6"
                  />
                  {[...Array(10)].map((_, i) => {
                    const angle = (i / 10) * Math.PI * 2 + (poi.id.charCodeAt(0) * 0.4);
                    const radius = 10 + ((i * 9 + poi.id.charCodeAt(1)) % 22);
                    const blobX = x + Math.cos(angle) * radius;
                    const blobY = y + Math.sin(angle) * radius;
                    const r = 2 + ((i * 3 + poi.id.charCodeAt(0)) % 3);
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
                const r = 14;
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
                  r="30"
                  fill="none"
                  stroke="#22d3ee"
                  strokeWidth="2"
                  opacity="0.6"
                />
              )}

              {/* POI icon */}
              <text
                x={x}
                y={y}
                textAnchor="middle"
                dominantBaseline="central"
                className={getPOIColor(poi.type)}
                style={{
                  fontSize: poi.type === 'sun' ? '40px' : poi.type === 'station' ? '21px' : '28px',
                  filter: isHovered ? 'brightness(1.4)' : undefined,
                }}
              >
                {getPOIIcon(poi.type)}
              </text>

              {/* POI label */}
              {poi.type !== 'sun' && (
                <text x={x} y={y - 25} fill="#9ca3af" fontSize="11" textAnchor="middle">
                  {poi.name}
                </text>
              )}

              {/* Resources display */}
              {poi.resources && (
                <g>
                  {poi.resources.map((res, idx) => (
                    <text key={idx} x={x + 40} y={y - 10 + idx * 14} fill="#d1d5db" fontSize="9">
                      {res.name} {res.amount}
                    </text>
                  ))}
                </g>
              )}
            </g>
          );
        })}

        {/* Jump Gates */}
        {jumpGates.map((gate) => {
          // Convert angle to radians (0° = North/Up, clockwise)
          const angleRad = (gate.angle - 90) * (Math.PI / 180);
          // Position jump gates outside the farthest orbit
          // Use the same scale calculation as the main view
          const maxPOIDistance = pois.length > 0
            ? Math.sqrt(Math.max(...pois.map((poi) => poi.x * poi.x + poi.y * poi.y)))
            : 0;
          const gateRadius = (maxPOIDistance || 1) * 1.15;
          const gateX = centerX + Math.cos(angleRad) * gateRadius * scale;
          const gateY = centerY + Math.sin(angleRad) * gateRadius * scale;

          const isGateClickable = !isTraveling && !!onJumpToSystem;
          const isGateHovered = hoveredGate === gate.id;

          return (
            <g
              key={gate.id}
              style={{ cursor: isGateClickable ? 'pointer' : 'default' }}
              onClick={() => { if (isGateClickable) { onJumpToSystem(gate.id); setActionMessage(`Jumping to ${gate.name}...`); } }}
              onMouseEnter={() => { if (isGateClickable) setHoveredGate(gate.id); }}
              onMouseLeave={() => setHoveredGate(null)}
            >
              {/* Jump gate icon */}
              <circle
                cx={gateX}
                cy={gateY}
                r="25"
                fill="none"
                stroke={isGateHovered ? '#22d3ee' : '#06b6d4'}
                strokeWidth={isGateHovered ? 3 : 2}
                opacity={isGateHovered ? 1 : 0.7}
              />
              <circle
                cx={gateX}
                cy={gateY}
                r="20"
                fill="none"
                stroke="#06b6d4"
                strokeWidth="1.5"
                opacity="0.5"
                strokeDasharray="4,2"
              />
              {/* Jump gate crosshair */}
              <circle cx={gateX} cy={gateY} r="8" fill="none" stroke="#22d3ee" strokeWidth="1.5" opacity="0.9" />
              <line x1={gateX - 12} y1={gateY} x2={gateX + 12} y2={gateY} stroke="#22d3ee" strokeWidth="1.5" opacity="0.9" />
              <line x1={gateX} y1={gateY - 12} x2={gateX} y2={gateY + 12} stroke="#22d3ee" strokeWidth="1.5" opacity="0.9" />
              {/* System name */}
              <text
                x={gateX}
                y={gateY - 35}
                fill="#06b6d4"
                fontSize="10"
                textAnchor="middle"
                className="font-mono"
              >
                {gate.name}
              </text>
              {/* System ID */}
              <text
                x={gateX}
                y={gateY + 40}
                fill="#4b5563"
                fontSize="8"
                textAnchor="middle"
                className="font-mono"
              >
                {gate.id}
              </text>
            </g>
          );
        })}

        {/* Player ship — rendered last so it's always on top */}
        {(() => {
          const currentPOI = pois.find((p) => p.id === player.location.poi || p.name === player.location.poi);
          if (!currentPOI) return null;
          const sx = currentPOI.x * scale + centerX + 10;
          const sy = -currentPOI.y * scale + centerY + 10;
          return (
            <text
              x={sx}
              y={sy}
              textAnchor="middle"
              dominantBaseline="central"
              fontSize="16"
            >
              🚀
            </text>
          );
        })()}
      </svg>

      <div className="absolute bottom-4 left-4 right-4 flex justify-between text-xs">
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
