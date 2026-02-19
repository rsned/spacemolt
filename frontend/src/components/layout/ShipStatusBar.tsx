import type { Player } from '../../types/game';
import { getEmpireTextColor, getBarColor } from '../../lib/utils';
import { useSystemDetails } from '../../lib/useSystemDetails';

interface ShipStatusBarProps {
  player: Player | null;
  isConnected: boolean;
}

export const ShipStatusBar: React.FC<ShipStatusBarProps> = ({ player, isConnected }) => {
  if (!isConnected || !player) {
    return (
      <div className="bg-spacemolt-panel border-b border-spacemolt-border p-6">
        <div className="flex flex-col items-center justify-center text-center">
          <svg className="w-16 h-16 text-gray-600 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
          </svg>
          <p className="text-gray-400 text-lg mb-1">No Agent Connected</p>
          <p className="text-gray-500 text-sm">Connect to an agent to view ship status</p>
        </div>
      </div>
    );
  }

  const hullPct = (player.hull / player.hullMax) * 100;
  const shieldPct = (player.shield / player.shieldMax) * 100;
  const fuelPct = (player.fuel / player.fuelMax) * 100;
  const cargoPct = (player.cargo / player.cargoMax) * 100;

  // Fetch accurate police level from knowledge base API
  const { policeLevel } = useSystemDetails(player.location.systemId || null);
  const isPoliced = policeLevel > 0;

  return (
    <div className="bg-spacemolt-panel border-b border-spacemolt-border p-3">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-4">
          <span className="text-cyan-400">🚀</span>
          <span className="font-semibold text-gray-100">{player.username}</span>
          <span className="text-gray-400">|</span>
          <span className="text-sm text-gray-300">{player.ship}</span>
          <span className={`text-xs uppercase ${getEmpireTextColor(player.empire)}`}>
            {player.empire}
          </span>
        </div>
      </div>

      <div className="flex items-center gap-6 text-sm">
        <div className="flex items-center gap-2">
          <span className="text-green-400">💰</span>
          <span className="font-mono text-green-300">{player.credits.toLocaleString()}cr</span>
        </div>

        <div className="flex items-center gap-2 flex-1">
          <span className="text-orange-400">⛽</span>
          <div className="w-24 h-2 bg-gray-700 rounded-full overflow-hidden">
            <div
              className={`h-full ${getBarColor(fuelPct, 'fuel')} transition-all`}
              style={{ width: `${fuelPct}%` }}
            />
          </div>
          <span className="text-xs text-gray-400 font-mono">
            {player.fuel}/{player.fuelMax}
          </span>
        </div>

        <div className="flex items-center gap-2 flex-1">
          <span className="text-red-400">🛡</span>
          <div className="w-24 h-2 bg-gray-700 rounded-full overflow-hidden">
            <div
              className={`h-full ${getBarColor(hullPct, 'hull')} transition-all`}
              style={{ width: `${hullPct}%` }}
            />
          </div>
          <span className="text-xs text-gray-400 font-mono">
            {player.hull}/{player.hullMax}
          </span>
        </div>

        <div className="flex items-center gap-2 flex-1">
          <span className="text-blue-400">🔰</span>
          <div className="w-24 h-2 bg-gray-700 rounded-full overflow-hidden">
            <div
              className={`h-full ${getBarColor(shieldPct, 'shield')} transition-all`}
              style={{ width: `${shieldPct}%` }}
            />
          </div>
          <span className="text-xs text-gray-400 font-mono">
            {player.shield}/{player.shieldMax}
          </span>
        </div>

        <div className="flex items-center gap-2 flex-1">
          <span className="text-amber-400">📦</span>
          <div className="w-24 h-2 bg-gray-700 rounded-full overflow-hidden">
            <div className={`h-full bg-amber-500 transition-all`} style={{ width: `${cargoPct}%` }} />
          </div>
          <span className="text-xs text-gray-400 font-mono">
            {player.cargo}/{player.cargoMax}
          </span>
        </div>
      </div>

      <div className="flex items-center justify-between mt-2 text-xs text-gray-400">
        <span>
          📍 {player.location.system} System &gt; {player.location.poi}
        </span>
        <span className={isPoliced ? 'text-green-400' : 'text-red-400'}>
          {isPoliced ? '🛡 Policed' : '☠ Lawless'}
        </span>
        <span className="font-mono">Tick: {player.tick}</span>
      </div>
    </div>
  );
};
