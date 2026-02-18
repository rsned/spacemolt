import { useState } from 'react';
import { ShipStatusBar } from './components/layout/ShipStatusBar';
import { SkillsPanel } from './components/layout/SkillsPanel';
import { ChatPanel } from './components/layout/ChatPanel';
import { NotificationFeed } from './components/layout/NotificationFeed';
import { GalaxyMap } from './components/galaxy/GalaxyMap';
import { SystemMap } from './components/system/SystemMap';
import { StationInterior } from './components/station/StationInterior';
import { MarketPanel } from './components/station/MarketPanel';
import { WorkshopPanel } from './components/station/WorkshopPanel';
import { ConnectionPanel } from './components/layout/ConnectionPanel';
import { useObserver } from './lib/useObserver';
import { useSystemMap } from './lib/useSystemMap';
import {
  mockPlayer,
  mockSkills,
  mockChat,
  mockNotifications,
  mockSystemPOIs,
  mockMarketOrders,
  mockRecipes,
  mockJumpGates,
} from './lib/mockData';

type ViewType = 'hud' | 'galaxy' | 'system' | 'station' | 'market' | 'workshop';

const WS_URL = `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/ws`;

function App() {
  const [activeView, setActiveView] = useState<ViewType>('hud');
  const observer = useObserver(WS_URL);

  const isLive = observer.status === 'connected' && observer.player !== null;
  const player = isLive ? observer.player : mockPlayer;
  const skills = isLive ? observer.skills : mockSkills;
  const systemMapData = useSystemMap(player?.location.systemId);

  return (
    <div className="min-h-screen bg-spacemolt-bg">
      {/* Top Navigation */}
      <div className="bg-spacemolt-panel border-b border-spacemolt-border p-4">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-4">
            <h1 className="font-sci-fi text-2xl text-cyan-400">SpaceMolt Observer</h1>
            <span className={`text-xs px-2 py-0.5 rounded ${
              isLive
                ? 'bg-green-900 text-green-400'
                : observer.status === 'connected'
                  ? 'bg-yellow-900 text-yellow-400'
                  : 'bg-gray-700 text-gray-400'
            }`}>
              {isLive ? `LIVE: ${observer.subscribedAgent}` : observer.status === 'connected' ? 'CONNECTED' : 'OFFLINE'}
            </span>
          </div>
          <div className="flex gap-2">
            {[
              { id: 'hud' as ViewType, label: 'HUD' },
              { id: 'galaxy' as ViewType, label: 'Galaxy Map' },
              { id: 'system' as ViewType, label: 'System Map' },
              { id: 'station' as ViewType, label: 'Station' },
              { id: 'market' as ViewType, label: 'Market' },
              { id: 'workshop' as ViewType, label: 'Workshop' },
            ].map((view) => (
              <button
                key={view.id}
                onClick={() => setActiveView(view.id)}
                className={`px-4 py-2 rounded transition-colors ${
                  activeView === view.id
                    ? 'bg-cyan-600 text-white'
                    : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                }`}
              >
                {view.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Global error banner */}
      {observer.error && (
        <div className="mx-4 mt-2 px-4 py-2 bg-red-900/70 border border-red-600 rounded text-red-300 text-sm font-mono flex items-center justify-between">
          <span>{observer.error}</span>
          <button
            onClick={() => observer.clearError()}
            className="ml-4 text-red-400 hover:text-red-200 text-xs"
          >
            dismiss
          </button>
        </div>
      )}

      {/* Content */}
      <div className="p-4">
        {activeView === 'hud' && (
          <div className="space-y-4">
            <ConnectionPanel observer={observer} />
            {player && (
              <>
                <ShipStatusBar player={player} />
                <div className="grid grid-cols-3 gap-4">
                  <div className="space-y-4">
                    <SkillsPanel skills={skills} />
                  </div>
                  <div>
                    <ChatPanel chat={mockChat} />
                  </div>
                  <div>
                    <NotificationFeed notifications={mockNotifications} />
                  </div>
                </div>
              </>
            )}
          </div>
        )}

        {activeView === 'galaxy' && <GalaxyMap />}

        {activeView === 'system' && (
          <SystemMap
            pois={systemMapData?.pois ?? mockSystemPOIs}
            player={player || mockPlayer}
            jumpGates={systemMapData?.jumpGates ?? mockJumpGates}
            policeLevel={systemMapData?.policeLevel ?? 0}
            onTravelToPOI={isLive ? (poiId) => observer.sendCommand('travel', { target_poi: poiId }) : undefined}
            onJumpToSystem={isLive ? (systemId) => observer.sendCommand('jump', { target_system: systemId }) : undefined}
          />
        )}

        {activeView === 'station' && <StationInterior player={player || mockPlayer} />}

        {activeView === 'market' && (
          <div className="max-w-3xl">
            <MarketPanel player={player || mockPlayer} orders={mockMarketOrders} />
          </div>
        )}

        {activeView === 'workshop' && (
          <div className="max-w-3xl">
            <WorkshopPanel recipes={mockRecipes} />
          </div>
        )}
      </div>
    </div>
  );
}

export default App;
