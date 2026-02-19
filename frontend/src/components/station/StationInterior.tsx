import type { Player } from '../../types/game';
import { useStationData } from '../../lib/useStationData';

interface StationInteriorProps {
  player: Player;
  onUndock?: () => void;
  onRefuel?: () => void;
  onRepair?: () => void;
  onNavigate?: (view: string) => void;
}

const ShieldIcon = () => (
  <svg viewBox="0 0 24 24" fill="currentColor" className="w-8 h-8 text-white inline-block">
    <path d="M12 2L3 7v5c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V7l-9-5z" />
  </svg>
);

const serviceConfig: Record<string, { icon: React.ReactNode; label: string }> = {
  market:    { icon: '🏪', label: 'Market' },
  crafting:  { icon: '🔧', label: 'Workshop' },
  shipyard:  { icon: '🚢', label: 'Shipyard' },
  missions:  { icon: '📋', label: 'Missions' },
  cloning:   { icon: '🧬', label: 'Cloning' },
  insurance: { icon: <ShieldIcon />, label: 'Insurance' },
  storage:   { icon: '📦', label: 'Storage' },
};

function StatBar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.round((value / max) * 100) : 0;
  return (
    <div className="w-full bg-gray-800 rounded-full h-3 overflow-hidden">
      <div
        className={`h-full rounded-full transition-all ${color}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

export const StationInterior: React.FC<StationInteriorProps> = ({
  player,
  onUndock,
  onRefuel,
  onRepair,
  onNavigate,
}) => {
  const poiId = player.location.dockedAt || null;

  if (!poiId) {
    return (
      <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg p-12">
        <div className="text-center">
          <h2 className="font-sci-fi text-cyan-400 text-2xl mb-4">STATION INTERIOR</h2>
          <div className="text-6xl mb-4">🚀</div>
          <div className="text-gray-400 text-lg">You must dock at a station to access its facilities</div>
          <div className="text-gray-500 text-sm mt-2">Use the System Map to find the nearest station</div>
        </div>
      </div>
    );
  }

  const { data: stationData, loading, error } = useStationData(poiId);

  if (loading) {
    return (
      <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg p-6">
        <h2 className="font-sci-fi text-cyan-400 text-center mb-6">STATION INTERIOR</h2>
        <div className="text-center text-gray-400">Loading station data...</div>
      </div>
    );
  }

  if (error || !stationData) {
    return (
      <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg p-6">
        <h2 className="font-sci-fi text-cyan-400 text-center mb-6">STATION INTERIOR</h2>
        <div className="text-center text-red-400">
          {error || 'No station data available'}
        </div>
      </div>
    );
  }

  // Filter available services (excluding refuel/repair which go in the ship panel)
  const availableServices = Object.entries(stationData.services)
    .filter(([key, enabled]) => enabled && key !== 'refuel' && key !== 'repair' && key in serviceConfig)
    .map(([key]) => key);

  const midPoint = Math.ceil(availableServices.length / 2);
  const topRow = availableServices.slice(0, midPoint);
  const bottomRow = availableServices.slice(midPoint);

  const hasRefuel = stationData.services.refuel === true;
  const hasRepair = stationData.services.repair === true;
  const fuelFull = player.fuel >= player.fuelMax;
  const hullFull = player.hull >= player.hullMax;

  return (
    <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg p-6">
      {/* Header */}
      <div className="flex justify-between items-center mb-6">
        <h2 className="font-sci-fi text-cyan-400 text-xl">STATION INTERIOR</h2>
        <div className="text-right">
          <div className="text-sm text-gray-400">Station</div>
          <div className="text-lg text-white">{stationData.name}</div>
        </div>
      </div>

      {/* Main corridor layout */}
      <div className="flex items-stretch gap-4">
        {/* Left: Ship Panel */}
        <div className="flex-shrink-0 w-44 flex flex-col gap-3">
          <div className="bg-spacemolt-panel border-2 border-cyan-700 rounded-lg p-4 text-center flex-1 flex flex-col justify-center">
            <div className="text-gray-400 mb-1 text-xs tracking-widest">DOCKED</div>
            <div className="text-4xl mb-2">🚀</div>
            <div className="text-xs text-gray-300 truncate">{player.ship}</div>
          </div>

          {/* Refuel */}
          {hasRefuel && (
            <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs text-yellow-400">⛽ Fuel</span>
                <span className="text-xs text-gray-400">{player.fuel}/{player.fuelMax}</span>
              </div>
              <StatBar value={player.fuel} max={player.fuelMax} color="bg-yellow-500" />
              <button
                onClick={onRefuel}
                disabled={!onRefuel || fuelFull}
                className={`w-full mt-2 px-3 py-1.5 rounded text-xs font-bold transition-colors ${
                  fuelFull
                    ? 'bg-gray-700 text-gray-500 cursor-not-allowed'
                    : 'bg-yellow-700 hover:bg-yellow-600 text-yellow-100'
                }`}
              >
                {fuelFull ? 'FULL' : 'REFUEL'}
              </button>
            </div>
          )}

          {/* Repair */}
          {hasRepair && (
            <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs text-green-400">🔩 Hull</span>
                <span className="text-xs text-gray-400">{player.hull}/{player.hullMax}</span>
              </div>
              <StatBar value={player.hull} max={player.hullMax} color="bg-green-500" />
              <button
                onClick={onRepair}
                disabled={!onRepair || hullFull}
                className={`w-full mt-2 px-3 py-1.5 rounded text-xs font-bold transition-colors ${
                  hullFull
                    ? 'bg-gray-700 text-gray-500 cursor-not-allowed'
                    : 'bg-green-700 hover:bg-green-600 text-green-100'
                }`}
              >
                {hullFull ? 'FULL' : 'REPAIR'}
              </button>
            </div>
          )}
        </div>

        {/* Center: Service Panels */}
        <div className="flex-1 flex flex-col justify-center gap-2 relative min-h-[200px]">
          {/* Top row */}
          <div className="flex gap-3 justify-center">
            {topRow.map((svc) => {
              const cfg = serviceConfig[svc];
              return (
                <button
                  key={svc}
                  onClick={() => onNavigate?.(svc === 'crafting' ? 'workshop' : svc)}
                  className="bg-spacemolt-panel border border-spacemolt-border hover:border-cyan-600 hover:bg-cyan-900/20 rounded-lg p-5 min-w-[144px] transition-colors text-center group"
                >
                  <div className="text-3xl mb-1.5 group-hover:scale-110 transition-transform">{cfg.icon}</div>
                  <div className="text-sm text-gray-300 group-hover:text-cyan-300">{cfg.label}</div>
                </button>
              );
            })}
          </div>

          {/* Central walkway */}
          <div className="h-6 bg-gray-800/50 rounded mx-4 flex items-center justify-center">
            <div className="text-gray-600 text-xs tracking-[0.3em]">CORRIDOR</div>
          </div>

          {/* Bottom row */}
          <div className="flex gap-3 justify-center">
            {bottomRow.map((svc) => {
              const cfg = serviceConfig[svc];
              return (
                <button
                  key={svc}
                  onClick={() => onNavigate?.(svc === 'crafting' ? 'workshop' : svc)}
                  className="bg-spacemolt-panel border border-spacemolt-border hover:border-cyan-600 hover:bg-cyan-900/20 rounded-lg p-5 min-w-[144px] transition-colors text-center group"
                >
                  <div className="text-3xl mb-1.5 group-hover:scale-110 transition-transform">{cfg.icon}</div>
                  <div className="text-sm text-gray-300 group-hover:text-cyan-300">{cfg.label}</div>
                </button>
              );
            })}
          </div>

          {availableServices.length === 0 && (
            <div className="text-center text-gray-500 py-8">No services available at this station</div>
          )}
        </div>

        {/* Right: Undock */}
        <div className="flex-shrink-0 w-36 flex flex-col">
          <button
            onClick={onUndock}
            disabled={!onUndock}
            className={`w-full flex-1 rounded-lg p-4 transition-colors flex flex-col items-center justify-center gap-2 ${
              onUndock
                ? 'bg-red-900/30 border-2 border-red-700 hover:bg-red-900/50 text-red-400 hover:text-red-300'
                : 'bg-gray-800/30 border-2 border-gray-700 text-gray-600 cursor-not-allowed'
            }`}
          >
            <div className="text-3xl">🚪</div>
            <div className="text-sm font-bold tracking-widest">UNDOCK</div>
            <div className="text-xs text-gray-500 mt-1">AIRLOCK</div>
          </button>
        </div>
      </div>
    </div>
  );
};
