import type { Player } from '../../types/game';

interface StoragePanelProps {
  player: Player;
}

export const StoragePanel: React.FC<StoragePanelProps> = ({ player }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 mb-4">STORAGE</h3>

      <div className="bg-gray-800 rounded-lg p-4 border border-gray-700 mb-6">
        <div className="text-sm text-gray-400 mb-1">Ship Cargo</div>
        <div className="flex items-center justify-between mt-2">
          <div>
            <div className="text-white">{player.cargo} / {player.cargoMax}</div>
            <div className="text-xs text-gray-500">units used</div>
          </div>
          <span className="text-2xl">📦</span>
        </div>
        <div className="w-full bg-gray-700 rounded-full h-2 mt-3 overflow-hidden">
          <div
            className="h-full rounded-full bg-amber-500 transition-all"
            style={{ width: `${player.cargoMax > 0 ? Math.round((player.cargo / player.cargoMax) * 100) : 0}%` }}
          />
        </div>
      </div>

      <div className="text-sm text-gray-400 mb-3">STATION STORAGE:</div>
      <div className="text-center text-gray-500 py-8 bg-gray-800/50 rounded-lg border border-gray-700">
        Station storage contents will appear here when connected to a station with storage.
      </div>
    </div>
  );
};
