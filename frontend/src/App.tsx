import { useState, useEffect, useRef } from 'react';
import { ShipStatusBar } from './components/layout/ShipStatusBar';
import { SkillsPanel } from './components/layout/SkillsPanel';
import { ChatPanel } from './components/layout/ChatPanel';
import { NotificationFeed } from './components/layout/NotificationFeed';
import { GalaxyMap } from './components/galaxy/GalaxyMap';
import { SystemMap } from './components/system/SystemMap';
import { StationInterior } from './components/station/StationInterior';
import { MarketPanel } from './components/station/MarketPanel';
import { WorkshopPanel } from './components/station/WorkshopPanel';
import { ShipyardPanel } from './components/station/ShipyardPanel';
import { MissionsPanel } from './components/station/MissionsPanel';
import { CloningPanel } from './components/station/CloningPanel';
import { InsurancePanel } from './components/station/InsurancePanel';
import { StoragePanel } from './components/station/StoragePanel';
import { ConnectionPanel } from './components/layout/ConnectionPanel';
import { useObserver } from './lib/useObserver';
import { useSystemMap } from './lib/useSystemMap';
import {
  mockPlayer,
  mockSystemPOIs,
  mockMarketOrders,
  mockRecipes,
  mockJumpGates,
} from './lib/mockData';

type ViewType = 'hud' | 'galaxy' | 'system' | 'station' | 'market' | 'workshop' | 'shipyard' | 'missions' | 'cloning' | 'insurance' | 'storage';

const WS_URL = `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/ws`;

function App() {
  const [activeView, setActiveView] = useState<ViewType>('hud');
  const observer = useObserver(WS_URL);

  const [pendingDock, setPendingDock] = useState(false);
  const prevDockedRef = useRef<string | null>(null);

  const isLive = observer.status === 'connected' && observer.player !== null;
  const player = observer.player;
  const skills = observer.skills;
  const systemMapData = useSystemMap(player?.location.systemId);

  // Auto-dock after travel to a station completes
  useEffect(() => {
    if (!pendingDock) return;
    if (player?.traveling) return; // still traveling
    observer.sendCommand('dock', {});
    setPendingDock(false);
  }, [pendingDock, player?.traveling]);

  // Auto-switch to Station tab when docked
  useEffect(() => {
    if (player?.location.dockedAt && activeView === 'system') {
      setActiveView('station');
    }
  }, [player?.location.dockedAt]);

  // Auto-switch to System Map on undock (dockedAt transitions from truthy to null)
  useEffect(() => {
    const currentDocked = player?.location.dockedAt ?? null;
    if (prevDockedRef.current && !currentDocked) {
      setActiveView('system');
    }
    prevDockedRef.current = currentDocked;
  }, [player?.location.dockedAt]);

  // Station action handlers
  const handleUndock = () => observer.sendCommand('undock', {});
  const handleRefuel = () => observer.sendCommand('refuel', {});
  const handleRepair = () => observer.sendCommand('repair', {});
  const stationSubViews: ViewType[] = ['station', 'market', 'workshop', 'shipyard', 'missions', 'cloning', 'insurance', 'storage'];
  const isStationArea = stationSubViews.includes(activeView);
  const handleStationNavigate = (view: string) => {
    if (stationSubViews.includes(view as ViewType)) {
      setActiveView(view as ViewType);
    }
  };

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
              { id: 'hud' as ViewType, label: 'Home' },
              { id: 'galaxy' as ViewType, label: 'Galaxy Map' },
              { id: 'system' as ViewType, label: 'System Map' },
              { id: 'station' as ViewType, label: 'Station' },
            ].map((view) => (
              <button
                key={view.id}
                onClick={() => setActiveView(view.id)}
                className={`px-4 py-2 rounded transition-colors ${
                  view.id === 'station'
                    ? (isStationArea
                      ? 'bg-cyan-600 text-white px-6'
                      : 'bg-gray-700 text-gray-400 hover:bg-gray-600 px-6')
                    : (activeView === view.id
                      ? 'bg-cyan-600 text-white'
                      : 'bg-gray-700 text-gray-400 hover:bg-gray-600')
                }`}
              >
                {view.label}
              </button>
            ))}
          </div>
        </div>

        {/* Station sub-tabs */}
        {isStationArea && (
          <div className="flex gap-1.5 mt-2 ml-auto justify-end">
            {[
              { id: 'station' as ViewType, label: 'Overview' },
              { id: 'cloning' as ViewType, label: 'Cloning' },
              { id: 'insurance' as ViewType, label: 'Insurance' },
              { id: 'market' as ViewType, label: 'Market' },
              { id: 'missions' as ViewType, label: 'Missions' },
              { id: 'shipyard' as ViewType, label: 'Shipyard' },
              { id: 'storage' as ViewType, label: 'Storage' },
              { id: 'workshop' as ViewType, label: 'Workshop' },
            ].map((view) => (
              <button
                key={view.id}
                onClick={() => setActiveView(view.id)}
                className={`px-3 py-1.5 rounded text-sm transition-colors ${
                  activeView === view.id
                    ? 'bg-cyan-700 text-white'
                    : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                }`}
              >
                {view.label}
              </button>
            ))}
          </div>
        )}
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

      {/* Combat Alert Dialog */}
      {observer.combatAlert && isLive && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="bg-gray-900 border-2 border-red-600 rounded-lg p-6 max-w-md w-full mx-4 shadow-2xl shadow-red-900/50">
            <div className="flex items-center gap-3 mb-4">
              <span className="text-3xl">{observer.combatAlert.isBoss ? '💀' : '☠️'}</span>
              <div>
                <h2 className="font-sci-fi text-xl text-red-400">HOSTILE DETECTED</h2>
                <p className="text-red-300 text-sm font-mono">{observer.combatAlert.message}</p>
              </div>
            </div>
            <div className="bg-gray-800 rounded p-3 mb-4 text-sm space-y-1">
              <div className="flex justify-between">
                <span className="text-gray-400">Hostile:</span>
                <span className="text-red-300 font-mono">{observer.combatAlert.pirateName}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-gray-400">Tier:</span>
                <span className={`font-mono ${
                  observer.combatAlert.pirateTier === 'boss' ? 'text-purple-400' :
                  observer.combatAlert.pirateTier === 'large' ? 'text-orange-400' :
                  observer.combatAlert.pirateTier === 'medium' ? 'text-yellow-400' :
                  'text-gray-300'
                }`}>{observer.combatAlert.pirateTier.toUpperCase()}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-gray-400">Attack in:</span>
                <span className="text-red-400 font-mono animate-pulse">{observer.combatAlert.delayTicks} tick{observer.combatAlert.delayTicks !== 1 ? 's' : ''}</span>
              </div>
            </div>
            <div className="flex gap-3">
              <button
                onClick={() => {
                  // Flee: jump to first available gate
                  const gates = systemMapData?.jumpGates ?? [];
                  if (gates.length > 0) {
                    observer.sendCommand('jump', { target_system: gates[0].id });
                  }
                  observer.dismissCombatAlert();
                }}
                className="flex-1 px-4 py-3 bg-yellow-700 hover:bg-yellow-600 text-white rounded font-sci-fi text-lg transition-colors"
              >
                FLEE
              </button>
              <button
                onClick={() => {
                  observer.dismissCombatAlert();
                }}
                className="flex-1 px-4 py-3 bg-red-700 hover:bg-red-600 text-white rounded font-sci-fi text-lg transition-colors"
              >
                FIGHT
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Content */}
      <div className="p-4">
        {activeView === 'hud' && (
          <div className="space-y-4">
            <ConnectionPanel observer={observer} />
            <ShipStatusBar player={player} isConnected={isLive} />
            <div className="grid grid-cols-3 gap-4">
              <div className="space-y-4">
                <SkillsPanel skills={skills} isConnected={isLive} />
              </div>
              <div>
                <ChatPanel chat={[]} isConnected={isLive} />
              </div>
              <div>
                <NotificationFeed notifications={[]} isConnected={isLive} />
              </div>
            </div>
          </div>
        )}

        {activeView === 'galaxy' && <GalaxyMap />}

        {activeView === 'system' && (
          <SystemMap
            pois={systemMapData?.pois ?? mockSystemPOIs}
            player={player || mockPlayer}
            jumpGates={systemMapData?.jumpGates ?? mockJumpGates}
            policeLevel={systemMapData?.policeLevel ?? 0}
            onTravelToPOI={isLive ? (poiId, poiType) => {
              observer.sendCommand('travel', { target_poi: poiId });
              if (poiType === 'station') {
                setPendingDock(true);
              }
            } : undefined}
            onJumpToSystem={isLive ? (systemId) => observer.sendCommand('jump', { target_system: systemId }) : undefined}
          />
        )}

        {activeView === 'station' && (
          <StationInterior
            player={player || mockPlayer}
            onUndock={isLive ? handleUndock : undefined}
            onRefuel={isLive ? handleRefuel : undefined}
            onRepair={isLive ? handleRepair : undefined}
            onNavigate={handleStationNavigate}
          />
        )}

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

        {activeView === 'shipyard' && (
          <ShipyardPanel
            player={player || mockPlayer}
            myShips={observer.myShips}
            catalogShips={observer.catalogShips}
            activeShipId={observer.activeShipId}
            onCommand={isLive ? observer.sendCommand : () => {}}
          />
        )}

        {activeView === 'missions' && (
          <div className="max-w-3xl">
            <MissionsPanel />
          </div>
        )}

        {activeView === 'cloning' && (
          <div className="max-w-3xl">
            <CloningPanel player={player || mockPlayer} />
          </div>
        )}

        {activeView === 'insurance' && (
          <div className="max-w-3xl">
            <InsurancePanel player={player || mockPlayer} />
          </div>
        )}

        {activeView === 'storage' && (
          <StoragePanel
            player={player || mockPlayer}
            onCommand={isLive ? observer.sendCommand : () => {}}
            cargoData={observer.cargoData}
            storageData={observer.storageData}
            factionStorageData={observer.factionStorageData}
          />
        )}
      </div>
    </div>
  );
}

export default App;
