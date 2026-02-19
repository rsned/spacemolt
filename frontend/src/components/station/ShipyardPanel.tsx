import type { Player } from '../../types/game';

interface ShipyardPanelProps {
  player: Player;
}

export const ShipyardPanel: React.FC<ShipyardPanelProps> = ({ player }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 mb-4">SHIPYARD</h3>

      <div className="grid grid-cols-2 gap-4 mb-6">
        <div className="bg-gray-800 rounded-lg p-4 border border-gray-700">
          <div className="text-sm text-gray-400 mb-1">Current Ship</div>
          <div className="text-lg text-white">{player.ship}</div>
          <div className="text-xs text-gray-500 mt-1">Class: {player.shipClass}</div>
        </div>
        <div className="bg-gray-800 rounded-lg p-4 border border-gray-700">
          <div className="text-sm text-gray-400 mb-1">Ship Stats</div>
          <div className="text-xs space-y-1 mt-2">
            <div className="flex justify-between">
              <span className="text-gray-400">Hull:</span>
              <span className="text-white">{player.hull}/{player.hullMax}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-gray-400">Shield:</span>
              <span className="text-white">{player.shield}/{player.shieldMax}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-gray-400">Cargo:</span>
              <span className="text-white">{player.cargo}/{player.cargoMax}</span>
            </div>
          </div>
        </div>
      </div>

      <div className="text-sm text-gray-400 mb-3">AVAILABLE SHIPS:</div>
      <div className="text-center text-gray-500 py-8 bg-gray-800/50 rounded-lg border border-gray-700">
        Ship listings will appear here when connected to a station shipyard.
      </div>
    </div>
  );
};
