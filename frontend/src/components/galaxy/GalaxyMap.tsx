import type { GalaxySystem } from '../../types/game';
import { getEmpireColor } from '../../lib/utils';

interface GalaxyMapProps {
  systems: GalaxySystem[];
}

export const GalaxyMap: React.FC<GalaxyMapProps> = ({ systems }) => {
  return (
    <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg relative overflow-hidden" style={{ height: '900px' }}>
      <div className="absolute top-4 left-4 z-10 flex gap-4 items-center bg-spacemolt-panel p-3 rounded-lg border border-spacemolt-border">
        <h2 className="font-sci-fi text-cyan-400">◄ GALAXY MAP</h2>
        <input type="text" placeholder="Search..." className="bg-gray-800 border border-gray-700 rounded px-3 py-1 text-sm w-40" />
        <button className="text-cyan-400 hover:text-cyan-300">⟳</button>
      </div>

      <div className="absolute top-4 right-4 z-10 bg-spacemolt-panel p-3 rounded-lg border border-spacemolt-border text-xs space-y-1">
        <div className="font-sci-fi text-cyan-400 mb-2">Empire Legend</div>
        {[
          { empire: 'Solarian', color: 'bg-yellow-500' },
          { empire: 'Voidborn', color: 'bg-purple-500' },
          { empire: 'Crimson', color: 'bg-red-500' },
          { empire: 'Nebula', color: 'bg-teal-500' },
          { empire: 'Outerrim', color: 'bg-green-500' },
        ].map((e, idx) => (
          <div key={idx} className="flex items-center gap-2">
            <div className={`w-3 h-3 rounded-full ${e.color}`} />
            <span className="text-gray-400">{e.empire}</span>
          </div>
        ))}
        <div className="flex items-center gap-2 mt-2 pt-2 border-t border-gray-700">
          <span className="text-yellow-400">★</span>
          <span className="text-gray-400">You are here</span>
        </div>
      </div>

      <svg className="w-full h-full">
        {/* Connections */}
        <line x1="100" y1="150" x2="200" y2="100" stroke="#374151" strokeWidth="1" />
        <line x1="100" y1="150" x2="150" y2="200" stroke="#374151" strokeWidth="1" />
        <line x1="200" y1="100" x2="250" y2="180" stroke="#374151" strokeWidth="1" />

        {/* Systems */}
        {systems.map((system) => (
          <g key={system.id}>
            {system.online > 0 && (
              <circle
                cx={system.x}
                cy={system.y}
                r="15"
                fill="none"
                stroke={getEmpireColor(system.empire)}
                strokeWidth="2"
                opacity="0.3"
                className="animate-pulse-slow"
              />
            )}
            <circle
              cx={system.x}
              cy={system.y}
              r="6"
              fill={getEmpireColor(system.empire)}
              className="cursor-pointer hover:scale-125 transition-transform"
            />
            <text
              x={system.x}
              y={system.y - 12}
              fill="#9ca3af"
              fontSize="10"
              textAnchor="middle"
              className="pointer-events-none"
            >
              {system.name}
            </text>
            {system.online > 0 && (
              <text
                x={system.x}
                y={system.y + 20}
                fill="#22c55e"
                fontSize="9"
                textAnchor="middle"
                className="pointer-events-none"
              >
                {system.online}
              </text>
            )}
            {system.id === 'zibal' && (
              <text x={system.x} y={system.y + 35} fill="#eab308" fontSize="14" textAnchor="middle">
                ★
              </text>
            )}
          </g>
        ))}
      </svg>

      <div className="absolute bottom-4 left-4 bg-spacemolt-panel p-3 rounded-lg border border-spacemolt-border text-xs">
        <span className="text-gray-400">Zoom:</span>
        <div className="w-32 h-1 bg-gray-700 rounded-full mt-1">
          <div className="w-1/2 h-full bg-cyan-500 rounded-full" />
        </div>
        <div className="mt-2 text-gray-400">Systems: 487 | Online: 23</div>
      </div>
    </div>
  );
};
