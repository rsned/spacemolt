import type { Player } from '../../types/game';

interface InsurancePanelProps {
  player: Player;
  onRefresh?: () => void;
}

export const InsurancePanel: React.FC<InsurancePanelProps> = ({ player, onRefresh }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <div className="flex items-center justify-between mb-4">
        <h3 className="font-sci-fi text-cyan-400">INSURANCE</h3>
        {onRefresh && (
          <button
            onClick={() => onRefresh()}
            className="text-xs text-gray-400 hover:text-cyan-400 border border-gray-600 hover:border-cyan-600 px-2 py-1 rounded"
          >
            REFRESH
          </button>
        )}
      </div>

      <div className="bg-gray-800 rounded-lg p-4 border border-gray-700 mb-6">
        <div className="text-sm text-gray-400 mb-1">Current Ship</div>
        <div className="flex items-center justify-between mt-2">
          <div>
            <div className="text-white">{player.ship}</div>
            <div className="text-xs text-gray-500">Class: {player.shipClass}</div>
          </div>
          <span className="text-2xl">🛡</span>
        </div>
      </div>

      <div className="text-sm text-gray-400 mb-3">INSURANCE POLICIES:</div>
      <div className="text-center text-gray-500 py-8 bg-gray-800/50 rounded-lg border border-gray-700">
        Insurance options will appear here when connected to a station with insurance services.
      </div>
    </div>
  );
};
