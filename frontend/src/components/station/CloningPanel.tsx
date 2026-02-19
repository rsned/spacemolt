import type { Player } from '../../types/game';

interface CloningPanelProps {
  player: Player;
}

export const CloningPanel: React.FC<CloningPanelProps> = ({ player }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 mb-4">CLONING FACILITY</h3>

      <div className="bg-gray-800 rounded-lg p-4 border border-gray-700 mb-6">
        <div className="text-sm text-gray-400 mb-1">Clone Status</div>
        <div className="flex items-center gap-3 mt-2">
          <span className="text-2xl">🧬</span>
          <div>
            <div className="text-white">{player.username}</div>
            <div className="text-xs text-gray-500">Clone location: {player.location.system}</div>
          </div>
        </div>
      </div>

      <div className="text-sm text-gray-400 mb-3">CLONE OPTIONS:</div>
      <div className="text-center text-gray-500 py-8 bg-gray-800/50 rounded-lg border border-gray-700">
        Cloning services will appear here when connected to a cloning facility.
      </div>
    </div>
  );
};
