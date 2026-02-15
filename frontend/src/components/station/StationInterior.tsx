import type { Player } from '../../types/game';
import { useState } from 'react';

interface StationInteriorProps {
  player: Player;
}

const services = [
  { id: 'refuel', name: 'Fuel Depot', icon: '⛽', color: 'text-orange-400' },
  { id: 'repair', name: 'Repair Bay', icon: '🔧', color: 'text-blue-400' },
  { id: 'market', name: 'Exchange', icon: '📊', color: 'text-green-400' },
  { id: 'storage', name: 'Storage', icon: '📦', color: 'text-amber-600' },
  { id: 'crafting', name: 'Workshop', icon: '⚒', color: 'text-purple-400' },
  { id: 'missions', name: 'Mission Board', icon: '📋', color: 'text-yellow-400' },
  { id: 'shipyard', name: 'Shipyard', icon: '🚀', color: 'text-white' },
  { id: 'quarters', name: 'Quarters', icon: '🏠', color: 'text-gray-300' },
];

export const StationInterior: React.FC<StationInteriorProps> = ({ player }) => {
  const [selectedService, setSelectedService] = useState<string | null>(null);

  return (
    <div className="bg-spacemolt-bg border border-spacemolt-border rounded-lg p-6">
      <h2 className="font-sci-fi text-cyan-400 text-center mb-6">STATION INTERIOR</h2>

      <div className="max-w-2xl mx-auto">
        {/* Docking Bay */}
        <div className="bg-spacemolt-panel border-2 border-cyan-700 rounded-lg p-8 text-center mb-4">
          <div className="text-gray-400 mb-2">DOCKING BAY</div>
          <div className="text-4xl mb-2">🚀</div>
          <div className="text-sm text-gray-500">Your Ship: {player.ship}</div>
        </div>

        {/* Central Aisle */}
        <div className="relative">
          {/* Aisle */}
          <div className="absolute left-1/2 top-0 bottom-0 w-16 bg-gray-800/50 transform -translate-x-1/2 rounded" />

          {/* Service Rooms */}
          <div className="flex flex-col gap-4 py-4">
            {Array.from({ length: Math.ceil(services.length / 2) }, (_, rowIdx) => {
              const leftService = services[rowIdx * 2];
              const rightService = services[rowIdx * 2 + 1];

              return (
                <div key={rowIdx} className="flex items-center gap-8">
                  <div className="w-64 flex justify-end">
                    {leftService && (
                      <button
                        onClick={() => setSelectedService(leftService.id)}
                        className={`w-60 bg-spacemolt-panel border border-spacemolt-border rounded-lg p-6 hover:border-cyan-500 hover:shadow-lg hover:shadow-cyan-500/30 transition-all ${
                          selectedService === leftService.id ? 'border-cyan-500 shadow-lg shadow-cyan-500/30' : ''
                        }`}
                      >
                        <div className={`text-4xl ${leftService.color} mb-1`}>{leftService.icon}</div>
                        <div className="text-base text-gray-300">{leftService.name}</div>
                      </button>
                    )}
                  </div>
                  <div className="w-16 flex-shrink-0" />
                  <div className="w-64">
                    {rightService && (
                      <button
                        onClick={() => setSelectedService(rightService.id)}
                        className={`w-60 bg-spacemolt-panel border border-spacemolt-border rounded-lg p-6 hover:border-cyan-500 hover:shadow-lg hover:shadow-cyan-500/30 transition-all ${
                          selectedService === rightService.id ? 'border-cyan-500 shadow-lg shadow-cyan-500/30' : ''
                        }`}
                      >
                        <div className={`text-4xl ${rightService.color} mb-1`}>{rightService.icon}</div>
                        <div className="text-base text-gray-300">{rightService.name}</div>
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Undock Exit */}
        <div className="mt-6">
          <button className="w-full bg-red-900/30 border border-red-700 hover:bg-red-900/50 rounded-lg p-4 text-red-400 transition-colors">
            UNDOCK EXIT
          </button>
        </div>
      </div>
    </div>
  );
};
