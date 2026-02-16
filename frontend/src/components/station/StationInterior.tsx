import type { Player } from '../../types/game';
import type { Facility } from '../../types/game';
import { useStationData } from '../../lib/useStationData';

interface StationInteriorProps {
  player: Player;
}

const categoryIcons: Record<string, { icon: string; color: string; label: string }> = {
  service: { icon: '🏪', color: 'text-blue-400', label: 'Services' },
  infrastructure: { icon: '🏗️', color: 'text-yellow-400', label: 'Infrastructure' },
  production: { icon: '🏭', color: 'text-orange-400', label: 'Production' },
  faction: { icon: '🏛️', color: 'text-purple-400', label: 'Faction' },
  personal: { icon: '🏠', color: 'text-green-400', label: 'Personal' },
  unknown: { icon: '❓', color: 'text-gray-400', label: 'Unknown' },
};

export const StationInterior: React.FC<StationInteriorProps> = ({ player }) => {
  // Get POI ID from player location
  const poiId = player.location.dockedAt || null;
  const { data: stationData, loading, error } = useStationData(poiId);

  // Group facilities by category
  const facilitiesByCategory = stationData?.facilities.reduce((acc, facility) => {
    if (!acc[facility.category]) {
      acc[facility.category] = [];
    }
    acc[facility.category].push(facility);
    return acc;
  }, {} as Record<string, Facility[]>) || {};

  const categories = Object.keys(facilitiesByCategory).sort();

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

  return (
    <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg p-6">
      <div className="flex justify-between items-center mb-6">
        <h2 className="font-sci-fi text-cyan-400 text-xl">STATION INTERIOR</h2>
        <div className="text-right">
          <div className="text-sm text-gray-400">Station</div>
          <div className="text-lg text-white">{stationData.name}</div>
        </div>
      </div>

      {/* Station Info */}
      <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4 mb-6">
        <div className="grid grid-cols-3 gap-4 text-sm">
          <div>
            <span className="text-gray-400">Empire:</span>
            <span className="ml-2 text-white capitalize">{stationData.empire}</span>
          </div>
          <div>
            <span className="text-gray-400">Defense:</span>
            <span className="ml-2 text-white">{stationData.defense_level}%</span>
          </div>
          <div>
            <span className="text-gray-400">Facilities:</span>
            <span className="ml-2 text-white">{stationData.facilities.length}</span>
          </div>
        </div>
      </div>

      {/* Docking Bay */}
      <div className="bg-spacemolt-panel border-2 border-cyan-700 rounded-lg p-6 text-center mb-6">
        <div className="text-gray-400 mb-2 text-sm">DOCKING BAY</div>
        <div className="text-4xl mb-2">🚀</div>
        <div className="text-sm text-gray-500">Your Ship: {player.ship}</div>
      </div>

      {/* Facilities by Category */}
      {categories.length === 0 ? (
        <div className="text-center text-gray-500 py-8">No facility data available</div>
      ) : (
        <div className="space-y-6">
          {categories.map((category) => {
            const categoryInfo = categoryIcons[category] || categoryIcons.unknown;
            const facilities = facilitiesByCategory[category];

            return (
              <div key={category} className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
                {/* Category Header */}
                <div className="flex items-center gap-3 p-2 border-b border-spacemolt-border">
                  <span className={`text-2xl ${categoryInfo.color}`}>{categoryInfo.icon}</span>
                  <span className={`text-lg ${categoryInfo.color}`}>{categoryInfo.label}</span>
                  <span className="text-gray-500 text-sm ml-auto">
                    {facilities.length} facility{facilities.length !== 1 ? 's' : ''}
                  </span>
                </div>

                {/* Facilities - Always shown */}
                <div className="mt-4 grid grid-cols-2 gap-3">
                  {facilities.map((facility) => (
                    <div
                      key={facility.id}
                      className="bg-spacemolt-bg border border-spacemolt-border rounded p-3"
                    >
                      <div className="text-sm text-gray-300">{facility.name}</div>
                      <div className="flex items-center gap-2 mt-1">
                        <span className="text-xs text-gray-500">Level</span>
                        <span className="text-xs text-white">{facility.level}</span>
                        {facility.level >= 5 && (
                          <span className="text-xs text-yellow-400">★★★★★</span>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Undock Exit */}
      <div className="mt-6">
        <button className="w-full bg-red-900/30 border border-red-700 hover:bg-red-900/50 rounded-lg p-4 text-red-400 transition-colors">
          UNDOCK EXIT
        </button>
      </div>
    </div>
  );
};
