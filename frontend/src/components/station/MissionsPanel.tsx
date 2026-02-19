interface MissionsPanelProps {
  // placeholder for future mission data
}

export const MissionsPanel: React.FC<MissionsPanelProps> = () => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 mb-4">MISSIONS</h3>

      <div className="flex gap-2 mb-4">
        {['AVAILABLE', 'ACTIVE', 'COMPLETED'].map((tab, idx) => (
          <button
            key={tab}
            className={`px-4 py-2 rounded text-sm ${
              idx === 0 ? 'bg-cyan-600 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      <div className="text-sm text-gray-400 mb-3">AVAILABLE MISSIONS:</div>
      <div className="text-center text-gray-500 py-8 bg-gray-800/50 rounded-lg border border-gray-700">
        Mission board will appear here when connected to a station with missions.
      </div>
    </div>
  );
};
