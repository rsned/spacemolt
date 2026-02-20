import { useState, useEffect, useRef } from 'react';
import type { Player, ShipClass, OwnedShip } from '../../types/game';

interface ShipyardPanelProps {
  player: Player;
  myShips: OwnedShip[];
  catalogShips: ShipClass[];
  activeShipId: string | null;
  onCommand: (command: string, payload: Record<string, unknown>) => void;
  onGetMyShips?: (force?: boolean) => void;
  onGetCatalogShips?: (force?: boolean) => void;
}

function meetsSkillRequirements(
  required: Record<string, number>,
  playerSkills: Record<string, { level: number; xp: number }>,
): boolean {
  return Object.entries(required).every(
    ([skill, level]) => (playerSkills[skill]?.level ?? 0) >= level,
  );
}

function formatCredits(n: number): string {
  return n.toLocaleString();
}

type SelectedShip =
  | { kind: 'owned'; ship: OwnedShip }
  | { kind: 'catalog'; ship: ShipClass };

export const ShipyardPanel: React.FC<ShipyardPanelProps> = ({
  player,
  myShips,
  catalogShips,
  activeShipId,
  onCommand,
  onGetMyShips,
  onGetCatalogShips,
}) => {
  const [selected, setSelected] = useState<SelectedShip | null>(null);
  const [hideUnqualified, setHideUnqualified] = useState(false);
  const [confirmSell, setConfirmSell] = useState<OwnedShip | null>(null);

  // Fetch owned ships on mount (cache-aware if available)
  useEffect(() => {
    if (onGetMyShips) {
      onGetMyShips();
    } else {
      onCommand('list_ships', {});
    }
  }, []);

  // Fetch catalog after owned ships arrive (cache-aware if available)
  const catalogFetched = useRef(false);
  useEffect(() => {
    if (myShips.length > 0 && !catalogFetched.current) {
      catalogFetched.current = true;
      if (onGetCatalogShips) {
        onGetCatalogShips();
      } else {
        onCommand('get_ships', {});
      }
    }
  }, [myShips]);

  const activeShip = myShips.find((s) => s.ship_id === activeShipId) ?? null;

  // Filtered catalog
  const filteredCatalog = hideUnqualified
    ? catalogShips.filter((s) => meetsSkillRequirements(s.required_skills ?? {}, player.skills))
    : catalogShips;

  return (
    <div className="flex gap-4 w-full">
      {/* Left Half */}
      <div className="w-1/2 space-y-4">
        {/* Active Ship */}
        <div className="bg-spacemolt-panel border-2 border-cyan-600 rounded-lg p-4">
          <div className="text-xs text-cyan-400 font-sci-fi mb-2">ACTIVE SHIP</div>
          {myShips.length === 0 ? (
            <div className="text-gray-500 text-sm animate-pulse">Loading...</div>
          ) : activeShip ? (
            <div>
              <div className="text-lg text-white font-bold">{activeShip.class_name}</div>
              <div className="text-xs text-gray-400 mb-2">Class: {activeShip.class_id}</div>
              <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                <StatRow label="Hull" value={activeShip.hull} />
                <StatRow label="Fuel" value={activeShip.fuel} />
                <StatRow label="Cargo" value={`${activeShip.cargo_used}`} />
                <StatRow label="Modules" value={`${activeShip.modules}`} />
              </div>
            </div>
          ) : (
            <div className="text-gray-500 text-sm">No active ship data</div>
          )}
        </div>

        {/* My Ships */}
        <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
          <div className="text-xs text-cyan-400 font-sci-fi mb-2">MY SHIPS ({myShips.length})</div>
          <div className="max-h-[200px] overflow-y-auto space-y-1">
            {myShips.length === 0 ? (
              <div className="text-gray-500 text-sm py-2 animate-pulse">Loading...</div>
            ) : (
              myShips.map((ship) => {
                const isActive = ship.ship_id === activeShipId;
                return (
                  <button
                    key={ship.ship_id}
                    onClick={() => setSelected({ kind: 'owned', ship })}
                    className={`w-full flex items-center justify-between px-3 py-2 rounded text-left transition-colors ${
                      selected?.kind === 'owned' && selected.ship.ship_id === ship.ship_id
                        ? 'bg-gray-700 border border-cyan-500'
                        : 'bg-gray-800/50 border border-transparent hover:bg-gray-700/50'
                    }`}
                  >
                    <div>
                      <div className="text-sm text-white font-bold">{ship.class_name}</div>
                      <div className="text-xs text-gray-400">{ship.class_id}</div>
                    </div>
                    {isActive ? (
                      <span className="text-xs text-cyan-400 font-sci-fi">Active</span>
                    ) : (
                      <span className="text-xs text-gray-500">{ship.location}</span>
                    )}
                  </button>
                );
              })
            )}
          </div>
        </div>

        {/* Ship Catalog */}
        <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
          <div className="flex items-center justify-between mb-2">
            <div className="text-xs text-cyan-400 font-sci-fi">SHIP CATALOG ({filteredCatalog.length})</div>
            <label className="flex items-center gap-1.5 text-xs text-gray-400 cursor-pointer">
              <input
                type="checkbox"
                checked={hideUnqualified}
                onChange={(e) => setHideUnqualified(e.target.checked)}
                className="accent-cyan-500"
              />
              Hide ships I can't fly
            </label>
          </div>
          <div className="max-h-[400px] overflow-y-auto space-y-1">
            {catalogShips.length === 0 ? (
              <div className="text-gray-500 text-sm py-4 text-center animate-pulse">Loading...</div>
            ) : filteredCatalog.length === 0 ? (
              <div className="text-gray-500 text-sm py-4 text-center">No ships match filter</div>
            ) : (
              filteredCatalog.map((ship) => {
                const canFly = meetsSkillRequirements(ship.required_skills ?? {}, player.skills);
                return (
                  <button
                    key={ship.id}
                    onClick={() => setSelected({ kind: 'catalog', ship })}
                    className={`w-full flex items-center justify-between px-3 py-2 rounded text-left transition-colors ${
                      selected?.kind === 'catalog' && selected.ship.id === ship.id
                        ? canFly
                          ? 'bg-gray-700 border border-cyan-500'
                          : 'opacity-50 bg-gray-700 border border-cyan-500'
                        : canFly
                          ? 'bg-gray-800/50 border border-transparent hover:bg-gray-700/50'
                          : 'opacity-40 border border-transparent hover:opacity-60'
                    }`}
                  >
                    <div>
                      <div className="text-sm text-white">{ship.name}</div>
                      <div className="text-xs text-gray-400">{ship.class}</div>
                    </div>
                    <div className="text-sm text-yellow-400 font-mono">{formatCredits(ship.price)} cr</div>
                  </button>
                );
              })
            )}
          </div>
        </div>
      </div>

      {/* Right Half — Detail Pane */}
      <div className="w-1/2">
        {!selected ? (
          <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-8 text-center text-gray-500">
            Select a ship to view details
          </div>
        ) : selected.kind === 'owned' ? (
          <OwnedShipDetail
            ship={selected.ship}
            shipClass={catalogShips.find((c) => c.id === selected.ship.class_id) ?? null}
            isActive={selected.ship.ship_id === activeShipId}
            onSwitch={() => onCommand('switch_ship', { ship_id: selected.ship.ship_id })}
            onSell={() => setConfirmSell(selected.ship)}
          />
        ) : (
          <CatalogShipDetail
            ship={selected.ship}
            player={player}
          onBuy={() => onCommand('buy_ship', { ship_class_id: selected.ship.id })}
          />
        )}
      </div>

      {/* Sell Confirm Dialog */}
      {confirmSell && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="bg-gray-900 border border-red-600 rounded-lg p-6 max-w-sm w-full mx-4 shadow-2xl">
            <h3 className="text-lg font-sci-fi text-red-400 mb-3">SELL SHIP</h3>
            <p className="text-gray-300 text-sm mb-4">
              Are you sure you want to sell <span className="text-white font-bold">{confirmSell.class_name}</span>?
              This cannot be undone.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={() => setConfirmSell(null)}
                className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 rounded transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={() => {
                  onCommand('sell_ship', { ship_id: confirmSell.ship_id });
                  setConfirmSell(null);
                  setSelected(null);
                }}
                className="px-4 py-2 bg-red-700 hover:bg-red-600 text-white rounded transition-colors"
              >
                Yes, Sell
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

function StatRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between">
      <span className="text-gray-400">{label}:</span>
      <span className="text-white font-mono">{value}</span>
    </div>
  );
}

function OwnedShipDetail({
  ship,
  shipClass,
  isActive,
  onSwitch,
  onSell,
}: {
  ship: OwnedShip;
  shipClass: ShipClass | null;
  isActive: boolean;
  onSwitch: () => void;
  onSell: () => void;
}) {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4 space-y-4">
      <div className="flex items-start justify-between">
        <div>
          <div className="text-lg text-white font-bold">{ship.class_name}</div>
          <div className="text-xs text-gray-400">Class: {ship.class_id}</div>
          <div className="text-xs text-cyan-400 mt-1">Your Ship</div>
        </div>
        {!isActive && (
          <button
            onClick={onSell}
            className="px-3 py-1 text-xs bg-red-900/50 hover:bg-red-800 text-red-400 hover:text-red-300 border border-red-700 rounded transition-colors"
          >
            Sell Ship
          </button>
        )}
      </div>

      {shipClass?.description && (
        <p className="text-sm text-gray-300">{shipClass.description}</p>
      )}

      {/* Current Status */}
      <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
        <StatRow label="Hull" value={ship.hull} />
        <StatRow label="Fuel" value={ship.fuel} />
        <StatRow label="Cargo Used" value={`${ship.cargo_used}`} />
        <StatRow label="Modules" value={`${ship.modules}`} />
        <StatRow label="Location" value={ship.location} />
      </div>

      {/* Class Specs (from catalog) */}
      {shipClass && (
        <>
          <div className="border-t border-spacemolt-border pt-3">
            <div className="text-xs text-gray-400 mb-2">Ship Class Specs</div>
            <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
              <StatRow label="Base Hull" value={`${shipClass.base_hull}`} />
              <StatRow label="Base Shield" value={`${shipClass.base_shield}`} />
              <StatRow label="Armor" value={`${shipClass.base_armor}`} />
              <StatRow label="Speed" value={`${shipClass.base_speed}`} />
              <StatRow label="Fuel Cap" value={`${shipClass.base_fuel}`} />
              <StatRow label="Cargo Cap" value={`${shipClass.cargo_capacity}`} />
              <StatRow label="CPU" value={`${shipClass.cpu_capacity}`} />
              <StatRow label="Power" value={`${shipClass.power_capacity}`} />
            </div>
          </div>

          {/* Slots */}
          <div className="flex gap-4 text-xs">
            <span className="text-gray-400">Defense: <span className="text-white">{shipClass.defense_slots}</span></span>
            <span className="text-gray-400">Utility: <span className="text-white">{shipClass.utility_slots}</span></span>
            <span className="text-gray-400">Weapon: <span className="text-white">{shipClass.weapon_slots}</span></span>
          </div>

          {/* Default Modules */}
          {shipClass.default_modules?.length > 0 && (
            <div>
              <div className="text-xs text-gray-400 mb-1">Default Modules:</div>
              <div className="flex flex-wrap gap-1">
                {shipClass.default_modules.map((mod, i) => (
                  <span key={i} className="px-2 py-0.5 bg-gray-800 border border-gray-700 rounded text-xs text-gray-300">
                    {mod}
                  </span>
                ))}
              </div>
            </div>
          )}
        </>
      )}

      {/* Action */}
      <div>
        {isActive ? (
          <button
            disabled
            className="w-full px-4 py-2 bg-gray-700 text-gray-500 rounded font-sci-fi cursor-not-allowed"
          >
            Currently Active
          </button>
        ) : (
          <button
            onClick={onSwitch}
            className="w-full px-4 py-2 bg-cyan-700 hover:bg-cyan-600 text-white rounded font-sci-fi transition-colors"
          >
            Switch to This Ship
          </button>
        )}
      </div>
    </div>
  );
}

function CatalogShipDetail({
  ship,
  player,
  onBuy,
}: {
  ship: ShipClass;
  player: Player;
  onBuy: () => void;
}) {
  const canAfford = player.credits >= ship.price;
  const hasSkills = meetsSkillRequirements(ship.required_skills ?? {}, player.skills);
  const canBuy = canAfford && hasSkills;

  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4 space-y-4">
      <div>
        <div className="text-lg text-white font-bold">{ship.name}</div>
        <div className="text-xs text-gray-400">Class: {ship.class}</div>
        {ship.description && (
          <p className="text-sm text-gray-300 mt-2">{ship.description}</p>
        )}
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
        <StatRow label="Hull" value={`${ship.base_hull}`} />
        <StatRow label="Shield" value={`${ship.base_shield}`} />
        <StatRow label="Armor" value={`${ship.base_armor}`} />
        <StatRow label="Speed" value={`${ship.base_speed}`} />
        <StatRow label="Fuel" value={`${ship.base_fuel}`} />
        <StatRow label="Cargo" value={`${ship.cargo_capacity}`} />
        <StatRow label="CPU" value={`${ship.cpu_capacity}`} />
        <StatRow label="Power" value={`${ship.power_capacity}`} />
      </div>

      {/* Slots */}
      <div className="flex gap-4 text-xs">
        <span className="text-gray-400">Defense: <span className="text-white">{ship.defense_slots}</span></span>
        <span className="text-gray-400">Utility: <span className="text-white">{ship.utility_slots}</span></span>
        <span className="text-gray-400">Weapon: <span className="text-white">{ship.weapon_slots}</span></span>
      </div>

      {/* Default Modules */}
      {ship.default_modules?.length > 0 && (
        <div>
          <div className="text-xs text-gray-400 mb-1">Default Modules:</div>
          <div className="flex flex-wrap gap-1">
            {ship.default_modules.map((mod, i) => (
              <span key={i} className="px-2 py-0.5 bg-gray-800 border border-gray-700 rounded text-xs text-gray-300">
                {mod}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Price */}
      <div className="flex items-center justify-between bg-gray-800/50 rounded p-3">
        <span className="text-sm text-gray-400">Price:</span>
        <span className={`text-lg font-mono font-bold ${canAfford ? 'text-yellow-400' : 'text-red-400'}`}>
          {formatCredits(ship.price)} cr
        </span>
      </div>

      {/* Required Skills */}
      {Object.keys(ship.required_skills ?? {}).length > 0 && (
        <div>
          <div className="text-xs text-gray-400 mb-1">Required Skills:</div>
          <div className="space-y-1">
            {Object.entries(ship.required_skills).map(([skill, level]) => {
              const playerLevel = player.skills[skill]?.level ?? 0;
              const met = playerLevel >= level;
              return (
                <div key={skill} className={`text-xs flex justify-between ${met ? 'text-green-400' : 'text-red-400'}`}>
                  <span>{skill}</span>
                  <span>Lv{level} {met ? '✓' : `(you: ${playerLevel})`}</span>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Buy Button */}
      <button
        onClick={onBuy}
        disabled={!canBuy}
        className={`w-full px-4 py-2 rounded font-sci-fi transition-colors ${
          canBuy
            ? 'bg-green-700 hover:bg-green-600 text-white'
            : 'bg-gray-700 text-gray-500 cursor-not-allowed'
        }`}
      >
        {!hasSkills ? 'Missing Required Skills' : !canAfford ? 'Insufficient Credits' : 'Buy Ship'}
      </button>
    </div>
  );
}
