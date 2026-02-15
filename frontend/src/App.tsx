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
import {
  mockPlayer,
  mockSkills,
  mockChat,
  mockNotifications,
  mockGalaxySystems,
  mockSystemPOIs,
  mockMarketOrders,
  mockRecipes,
  mockJumpGates,
} from './lib/mockData';

type ViewType = 'hud' | 'galaxy' | 'system' | 'station' | 'market' | 'workshop';

function App() {
  const [activeView, setActiveView] = useState<ViewType>('hud');

  return (
    <div className="min-h-screen bg-spacemolt-bg">
      {/* Top Navigation */}
      <div className="bg-spacemolt-panel border-b border-spacemolt-border p-4">
        <div className="flex items-center justify-between mb-4">
          <h1 className="font-sci-fi text-2xl text-cyan-400">SpaceMolt Observer</h1>
          <div className="flex gap-2">
            {[
              { id: 'hud' as ViewType, label: 'HUD Demo' },
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
        <p className="text-sm text-gray-500">
          Click the tabs above to view different UI components. All mockups are interactive.
        </p>
      </div>

      {/* Content */}
      <div className="p-4">
        {activeView === 'hud' && (
          <div className="space-y-4">
            <ShipStatusBar player={mockPlayer} />

            <div className="grid grid-cols-3 gap-4">
              <div className="space-y-4">
                <SkillsPanel skills={mockSkills} />
              </div>
              <div>
                <ChatPanel chat={mockChat} />
              </div>
              <div>
                <NotificationFeed notifications={mockNotifications} />
              </div>
            </div>
          </div>
        )}

        {activeView === 'galaxy' && <GalaxyMap systems={mockGalaxySystems} />}

        {activeView === 'system' && <SystemMap pois={mockSystemPOIs} player={mockPlayer} jumpGates={mockJumpGates} />}

        {activeView === 'station' && <StationInterior player={mockPlayer} />}

        {activeView === 'market' && (
          <div className="max-w-3xl">
            <MarketPanel player={mockPlayer} orders={mockMarketOrders} />
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
