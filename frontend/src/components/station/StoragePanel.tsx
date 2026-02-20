import { useState, useEffect } from 'react';
import type { Player, CargoData, StorageData, FactionStorageData, CargoItem } from '../../types/game';

interface StoragePanelProps {
  player: Player;
  onCommand: (command: string, payload: Record<string, unknown>) => void;
  onRefresh?: () => void;
  cargoData: CargoData | null;
  storageData: StorageData | null;
  factionStorageData: FactionStorageData | null;
}

type StorageSource = 'cargo' | 'station' | 'faction';
type SortMode = 'name-asc' | 'name-desc' | 'qty-asc' | 'qty-desc';

function sortItems(items: CargoItem[], mode: SortMode): CargoItem[] {
  const sorted = [...items];
  switch (mode) {
    case 'name-asc':
      return sorted.sort((a, b) => a.item_id.localeCompare(b.item_id));
    case 'name-desc':
      return sorted.sort((a, b) => b.item_id.localeCompare(a.item_id));
    case 'qty-asc':
      return sorted.sort((a, b) => a.quantity - b.quantity);
    case 'qty-desc':
      return sorted.sort((a, b) => b.quantity - a.quantity);
  }
}

function formatItemName(id: string): string {
  return id.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

export const StoragePanel: React.FC<StoragePanelProps> = ({
  player,
  onCommand,
  onRefresh,
  cargoData,
  storageData,
  factionStorageData,
}) => {
  const [source, setSource] = useState<StorageSource>('cargo');
  const [sort, setSort] = useState<SortMode>('name-asc');

  // Fetch cargo data on mount and when onCommand identity changes (e.g. reconnect)
  useEffect(() => {
    onCommand('get_cargo', {});
  }, [onCommand]);

  // Fetch station/faction storage only when docked
  const isDocked = !!player.location.dockedAt;
  useEffect(() => {
    if (!isDocked) return;
    onCommand('view_storage', {});
    onCommand('view_faction_storage', {});
  }, [onCommand, isDocked]);

  // Determine items for the selected source
  let items: CargoItem[] = [];
  let sourceLabel = '';
  switch (source) {
    case 'cargo':
      items = cargoData?.cargo ?? [];
      sourceLabel = 'Ship Cargo';
      break;
    case 'station':
      items = storageData?.items ?? [];
      sourceLabel = 'Station Storage';
      break;
    case 'faction':
      items = factionStorageData?.items ?? [];
      sourceLabel = `${factionStorageData?.faction_name ?? 'Faction'} Storage`;
      break;
  }

  const sorted = sortItems(items, sort);

  const cardClass = (active: boolean) =>
    `p-3 rounded-lg border cursor-pointer transition-colors ${
      active
        ? 'border-cyan-500 bg-cyan-950/30'
        : 'border-gray-700 bg-gray-800 hover:border-gray-500'
    }`;

  return (
    <div className="flex gap-4 w-full">
      {/* Left half — source selection */}
      <div className="w-1/2 space-y-3">
        <div className="flex items-center justify-between mb-3">
          <h3 className="font-sci-fi text-cyan-400">STORAGE SOURCES</h3>
          {onRefresh && (
            <button
              onClick={() => onRefresh()}
              className="text-xs text-gray-400 hover:text-cyan-400 border border-gray-600 hover:border-cyan-600 px-2 py-1 rounded"
            >
              REFRESH
            </button>
          )}
        </div>

        {/* Ship Cargo */}
        <div className={cardClass(source === 'cargo')} onClick={() => setSource('cargo')}>
          <div className="flex items-center justify-between mb-2">
            <span className="text-sm text-gray-300 font-medium">Ship Cargo</span>
            <span className="text-xs text-gray-500">
              {cargoData ? `${cargoData.used} / ${cargoData.capacity}` : `${player.cargo} / ${player.cargoMax}`}
            </span>
          </div>
          <div className="w-full bg-gray-700 rounded-full h-2 overflow-hidden">
            <div
              className="h-full rounded-full bg-amber-500 transition-all"
              style={{
                width: `${
                  cargoData
                    ? (cargoData.capacity > 0 ? Math.round((cargoData.used / cargoData.capacity) * 100) : 0)
                    : (player.cargoMax > 0 ? Math.round((player.cargo / player.cargoMax) * 100) : 0)
                }%`,
              }}
            />
          </div>
          <div className="text-xs text-gray-500 mt-1">
            {cargoData ? `${cargoData.cargo.length} item type${cargoData.cargo.length !== 1 ? 's' : ''}` : 'Loading...'}
          </div>
        </div>

        {/* Station Storage */}
        <div className={cardClass(source === 'station')} onClick={() => setSource('station')}>
          <div className="flex items-center justify-between mb-1">
            <span className="text-sm text-gray-300 font-medium">Station Storage</span>
          </div>
          {storageData ? (
            <div className="text-xs text-gray-500 space-y-0.5">
              <div>{storageData.items.length} item type{storageData.items.length !== 1 ? 's' : ''} stored</div>
              {storageData.credits > 0 && (
                <div className="text-amber-400">{storageData.credits.toLocaleString()} credits deposited</div>
              )}
            </div>
          ) : (
            <div className="text-xs text-gray-500">Loading...</div>
          )}
        </div>

        {/* Faction Storage — only shown if available */}
        {factionStorageData && (
          <div className={cardClass(source === 'faction')} onClick={() => setSource('faction')}>
            <div className="flex items-center justify-between mb-1">
              <span className="text-sm text-gray-300 font-medium">Faction Storage</span>
              <span className="text-xs px-1.5 py-0.5 bg-gray-700 rounded text-gray-400">
                [{factionStorageData.faction_tag}] {factionStorageData.faction_name}
              </span>
            </div>
            <div className="text-xs text-gray-500 space-y-0.5">
              <div>{factionStorageData.items.length} item type{factionStorageData.items.length !== 1 ? 's' : ''} stored</div>
              {factionStorageData.credits > 0 && (
                <div className="text-amber-400">{factionStorageData.credits.toLocaleString()} credits</div>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Right half — item list */}
      <div className="w-1/2 bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4 flex flex-col">
        <div className="flex items-center justify-between mb-3">
          <h3 className="font-sci-fi text-cyan-400">{sourceLabel}</h3>
          <div className="flex gap-1">
            {([
              { mode: 'name-asc' as SortMode, label: 'A-Z' },
              { mode: 'name-desc' as SortMode, label: 'Z-A' },
              { mode: 'qty-asc' as SortMode, label: 'Qty \u2191' },
              { mode: 'qty-desc' as SortMode, label: 'Qty \u2193' },
            ]).map(({ mode, label }) => (
              <button
                key={mode}
                onClick={() => setSort(mode)}
                className={`px-2 py-1 text-xs rounded transition-colors ${
                  sort === mode
                    ? 'bg-cyan-700 text-white'
                    : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                }`}
              >
                {label}
              </button>
            ))}
          </div>
        </div>

        <div className="flex-1 overflow-y-auto min-h-0 max-h-[60vh]">
          {sorted.length === 0 ? (
            <div className="text-center text-gray-500 py-8">
              No items in {sourceLabel.toLowerCase()}.
            </div>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-gray-500 text-xs border-b border-gray-700">
                  <th className="text-left py-1.5 font-normal">Item</th>
                  <th className="text-right py-1.5 font-normal">Qty</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((item) => (
                  <tr key={item.item_id} className="border-b border-gray-800 hover:bg-gray-800/50">
                    <td className="py-1.5 text-gray-300">{formatItemName(item.item_id)}</td>
                    <td className="py-1.5 text-right text-white font-mono">{item.quantity.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
};
