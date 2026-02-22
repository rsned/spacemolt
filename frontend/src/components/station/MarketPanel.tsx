import type { Player, MarketData, MarketItem, MarketOrderEntry } from '../../types/game';
import { useState, useEffect, useMemo } from 'react';
import { useCatalogItems } from '../../lib/useCatalogItems';

interface MarketPanelProps {
  player: Player;
  marketData: MarketData | null;
  onGetMarket: (force?: boolean) => void;
  onCommand: (command: string, payload: Record<string, unknown>) => void;
}

type MarketTab = 'buy' | 'sell';
type CategoryFilter = 'all' | 'component' | 'consumable' | 'ore' | 'refined' | 'crafted';
type SortField = 'item' | 'quantity' | 'price';
type SortDir = 'asc' | 'desc';

const CRAFTED_CATEGORIES = new Set(['artifact', 'contraband', 'defense', 'document', 'drone', 'material', 'misc', 'weapon', '']);

function matchesCategoryFilter(category: string, filter: CategoryFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'crafted') return CRAFTED_CATEGORIES.has(category);
  return category === filter;
}

const CATEGORY_FILTERS: { value: CategoryFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'component', label: 'Component' },
  { value: 'consumable', label: 'Consumable' },
  { value: 'ore', label: 'Ore' },
  { value: 'refined', label: 'Refined' },
  { value: 'crafted', label: 'Crafted Items' },
];

function formatItemName(item: MarketItem): string {
  if (item.item_name) return item.item_name;
  return item.item_id.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function formatCredits(n: number): string {
  return Math.round(n).toLocaleString();
}

function totalQuantity(orders: MarketOrderEntry[]): number {
  return orders.reduce((sum, o) => sum + o.quantity, 0);
}

export const MarketPanel: React.FC<MarketPanelProps> = ({
  player,
  marketData,
  onGetMarket,
  onCommand,
}) => {
  const [activeTab, setActiveTab] = useState<MarketTab>('sell');
  const [categoryFilter, setCategoryFilter] = useState<CategoryFilter>('all');
  const [showUnavailable, setShowUnavailable] = useState(false);
  const [sortField, setSortField] = useState<SortField>('item');
  const [sortDir, setSortDir] = useState<SortDir>('asc');
  const [search, setSearch] = useState('');
  const [selectedItemId, setSelectedItemId] = useState<string | null>(null);
  const { items: catalogItems } = useCatalogItems();

  useEffect(() => {
    onGetMarket();
  }, [onGetMarket]);

  // Build set of item IDs that have any orders (buy or sell)
  const marketItemIds = useMemo(() => {
    if (!marketData) return new Set<string>();
    return new Set(
      marketData.items
        .filter(item =>
          (item.sell_orders && item.sell_orders.length > 0) ||
          (item.buy_orders && item.buy_orders.length > 0),
        )
        .map(i => i.item_id),
    );
  }, [marketData]);

  // Filter items that have orders for the active tab, plus unavailable catalog items
  const filteredItems = useMemo(() => {
    if (!marketData) return [] as (MarketItem & { unavailable?: boolean })[];

    // Start with market items that have orders
    let items: (MarketItem & { unavailable?: boolean })[] = marketData.items
      .filter(item => {
        const orders = activeTab === 'sell' ? item.sell_orders : item.buy_orders;
        return orders && orders.length > 0;
      })
      .map(item => ({ ...item, unavailable: false }));

    // Add unavailable catalog items if toggled on
    if (showUnavailable && Object.keys(catalogItems).length > 0) {
      for (const cat of Object.values(catalogItems)) {
        if (!cat.tradeable) continue;
        if (marketItemIds.has(cat.id)) continue;
        items.push({
          item_id: cat.id,
          item_name: cat.name,
          sell_orders: [],
          buy_orders: [],
          best_sell: 0,
          best_buy: 0,
          unavailable: true,
        });
      }
    }

    // Apply category filter
    if (categoryFilter !== 'all') {
      items = items.filter(item => {
        const cat = catalogItems[item.item_id];
        const category = cat?.category ?? '';
        return matchesCategoryFilter(category, categoryFilter);
      });
    }

    // Apply search filter
    if (search) {
      const q = search.toLowerCase();
      items = items.filter(i =>
        i.item_id.toLowerCase().includes(q) ||
        formatItemName(i).toLowerCase().includes(q),
      );
    }

    // Sort
    items.sort((a, b) => {
      // Unavailable items always sort to bottom
      if (a.unavailable !== b.unavailable) return a.unavailable ? 1 : -1;

      let cmp = 0;
      const aOrders = activeTab === 'sell' ? a.sell_orders : a.buy_orders;
      const bOrders = activeTab === 'sell' ? b.sell_orders : b.buy_orders;
      switch (sortField) {
        case 'item':
          cmp = formatItemName(a).localeCompare(formatItemName(b));
          break;
        case 'quantity':
          cmp = totalQuantity(aOrders) - totalQuantity(bOrders);
          break;
        case 'price':
          cmp = (activeTab === 'sell' ? a.best_sell : a.best_buy) - (activeTab === 'sell' ? b.best_sell : b.best_buy);
          break;
      }
      return sortDir === 'asc' ? cmp : -cmp;
    });

    return items;
  }, [marketData, activeTab, search, sortField, sortDir, categoryFilter, showUnavailable, catalogItems, marketItemIds]);

  const sellCount = useMemo(
    () => marketData?.items.filter(i => i.sell_orders?.length > 0).length ?? 0,
    [marketData],
  );
  const buyCount = useMemo(
    () => marketData?.items.filter(i => i.buy_orders?.length > 0).length ?? 0,
    [marketData],
  );

  const selectedItem = useMemo(() => {
    if (!selectedItemId) return null;
    return filteredItems.find(i => i.item_id === selectedItemId) ?? null;
  }, [filteredItems, selectedItemId]);

  function handleSort(field: SortField) {
    if (sortField === field) {
      setSortDir(d => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortField(field);
      setSortDir('asc');
    }
  }

  function sortIndicator(field: SortField) {
    if (sortField !== field) return '';
    return sortDir === 'asc' ? ' \u25b2' : ' \u25bc';
  }

  const loading = marketData === null;

  return (
    <div className="flex gap-4 h-full">
      {/* Left pane - listing table */}
      <div className="flex-1 min-w-0 bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4 flex flex-col">
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-sci-fi text-cyan-400">MARKET EXCHANGE</h3>
          <button
            onClick={() => onGetMarket(true)}
            className="text-xs text-gray-400 hover:text-cyan-400 border border-gray-600 hover:border-cyan-600 px-2 py-1 rounded"
          >
            REFRESH
          </button>
        </div>

        {marketData?.base && (
          <div className="text-xs text-gray-500 mb-3">
            Station: <span className="text-gray-300">{marketData.base}</span>
          </div>
        )}

        {/* Tabs */}
        <div className="flex gap-2 mb-3">
          {(['sell', 'buy'] as MarketTab[]).map(tab => (
            <button
              key={tab}
              onClick={() => { setActiveTab(tab); setSelectedItemId(null); }}
              className={`px-4 py-1.5 rounded text-sm font-medium transition-colors ${
                tab === activeTab
                  ? tab === 'sell'
                    ? 'bg-green-700 text-white'
                    : 'bg-blue-700 text-white'
                  : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
              }`}
            >
              {tab === 'sell' ? 'SELL ORDERS' : 'BUY ORDERS'}
              <span className="ml-1.5 text-xs opacity-70">
                ({tab === 'sell' ? sellCount : buyCount})
              </span>
            </button>
          ))}
        </div>

        {/* Search */}
        <input
          type="text"
          placeholder="Search items..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-sm mb-3 w-full focus:border-cyan-600 focus:outline-none"
        />

        {/* Category filters */}
        <div className="flex flex-wrap gap-1.5 mb-3">
          {CATEGORY_FILTERS.map(f => (
            <button
              key={f.value}
              onClick={() => setCategoryFilter(f.value)}
              className={`px-2.5 py-1 rounded text-xs font-medium transition-colors ${
                categoryFilter === f.value
                  ? 'bg-cyan-700 text-white'
                  : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
              }`}
            >
              {f.label}
            </button>
          ))}
          <label className="flex items-center gap-1.5 ml-auto text-xs text-gray-400 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={showUnavailable}
              onChange={e => setShowUnavailable(e.target.checked)}
              className="accent-cyan-600"
            />
            Show unavailable
          </label>
        </div>

        {/* Table */}
        {loading ? (
          <div className="flex-1 flex items-center justify-center text-gray-500">
            Loading market data...
          </div>
        ) : filteredItems.length === 0 ? (
          <div className="flex-1 flex items-center justify-center text-gray-500">
            {search ? 'No items match your search' : `No ${activeTab} orders at this station`}
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-spacemolt-panel">
                <tr className="text-left text-gray-500 border-b border-gray-700">
                  <th className="pb-2 cursor-pointer hover:text-gray-300" onClick={() => handleSort('item')}>
                    Item{sortIndicator('item')}
                  </th>
                  <th className="pb-2 text-right cursor-pointer hover:text-gray-300" onClick={() => handleSort('quantity')}>
                    Qty{sortIndicator('quantity')}
                  </th>
                  <th className="pb-2 text-right cursor-pointer hover:text-gray-300" onClick={() => handleSort('price')}>
                    {activeTab === 'sell' ? 'Ask' : 'Bid'}{sortIndicator('price')}
                  </th>
                  <th className="pb-2 text-right">Orders</th>
                </tr>
              </thead>
              <tbody>
                {filteredItems.map(item => {
                  const orders = activeTab === 'sell' ? item.sell_orders : item.buy_orders;
                  const bestPrice = activeTab === 'sell' ? item.best_sell : item.best_buy;
                  const qty = totalQuantity(orders);
                  const isUnavailable = item.unavailable;
                  return (
                    <tr
                      key={item.item_id}
                      onClick={() => !isUnavailable && setSelectedItemId(item.item_id === selectedItemId ? null : item.item_id)}
                      className={`border-b border-gray-800 transition-colors ${
                        isUnavailable
                          ? 'cursor-default opacity-50'
                          : item.item_id === selectedItemId
                            ? 'bg-gray-700/50 cursor-pointer'
                            : 'hover:bg-gray-800/50 cursor-pointer'
                      }`}
                    >
                      <td className="py-2">
                        <span className={`font-mono ${isUnavailable ? 'text-gray-600' : 'text-cyan-300'}`}>{formatItemName(item)}</span>
                      </td>
                      <td className={`py-2 text-right font-mono ${isUnavailable ? 'text-gray-600' : 'text-white'}`}>
                        {isUnavailable ? '-' : qty.toLocaleString()}
                      </td>
                      <td className="py-2 text-right font-mono">
                        <span className={isUnavailable ? 'text-gray-600' : activeTab === 'sell' ? 'text-green-400' : 'text-blue-400'}>
                          {isUnavailable ? '-' : bestPrice > 0 ? `${formatCredits(bestPrice)}cr` : '-'}
                        </span>
                      </td>
                      <td className={`py-2 text-right font-mono ${isUnavailable ? 'text-gray-600' : 'text-gray-500'}`}>
                        {isUnavailable ? '-' : orders.length}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {/* Footer */}
        <div className="mt-3 pt-3 border-t border-gray-700 flex justify-between text-sm">
          <span className="text-gray-400">
            Credits: <span className="text-green-400 font-mono">{player.credits.toLocaleString()}cr</span>
          </span>
          <span className="text-gray-400">
            Cargo: <span className="text-amber-400 font-mono">{player.cargo}/{player.cargoMax}</span>
          </span>
        </div>
      </div>

      {/* Right pane - detail */}
      <div className="w-80 bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4 flex flex-col">
        {selectedItem ? (
          <ItemDetail item={selectedItem} activeTab={activeTab} onCommand={onCommand} />
        ) : (
          <div className="flex-1 flex items-center justify-center text-gray-500 text-sm">
            Select an item to view details
          </div>
        )}
      </div>
    </div>
  );
};

function ItemDetail({ item, activeTab, onCommand }: {
  item: MarketItem;
  activeTab: MarketTab;
  onCommand: (command: string, payload: Record<string, unknown>) => void;
}) {
  const orders = activeTab === 'sell' ? item.sell_orders : item.buy_orders;
  const bestPrice = activeTab === 'sell' ? item.best_sell : item.best_buy;
  const qty = totalQuantity(orders);

  const sorted = [...orders].sort((a, b) =>
    activeTab === 'sell' ? a.price_each - b.price_each : b.price_each - a.price_each,
  );

  const priceMin = sorted.length > 0 ? sorted[0].price_each : 0;
  const priceMax = sorted.length > 0 ? sorted[sorted.length - 1].price_each : 0;

  // Per-order quantity selection for buying from sell orders
  const [buyQtys, setBuyQtys] = useState<Record<number, number>>({});

  // Reset quantities when item changes
  useEffect(() => {
    setBuyQtys({});
  }, [item.item_id]);

  function adjustQty(orderIdx: number, delta: number, max: number) {
    setBuyQtys(prev => {
      const current = prev[orderIdx] ?? 0;
      const next = Math.max(0, Math.min(max, current + delta));
      if (next === 0) {
        const { [orderIdx]: _, ...rest } = prev;
        return rest;
      }
      return { ...prev, [orderIdx]: next };
    });
  }

  const totalBuyQty = Object.values(buyQtys).reduce((s, q) => s + q, 0);
  const totalBuyCost = sorted.reduce((s, order, idx) => s + (buyQtys[idx] ?? 0) * order.price_each, 0);

  function handleBuy() {
    if (totalBuyQty <= 0) return;
    onCommand('buy', { item_id: item.item_id, quantity: totalBuyQty });
    setBuyQtys({});
  }

  return (
    <>
      <h4 className="font-sci-fi text-cyan-400 mb-1">{formatItemName(item)}</h4>
      <div className="text-xs text-gray-500 mb-4">{item.item_id}</div>

      <div className="grid grid-cols-2 gap-3 mb-4">
        <div className="bg-gray-800/50 rounded p-2">
          <div className="text-xs text-gray-500 mb-1">Total Qty</div>
          <div className="font-mono text-lg text-white">{qty.toLocaleString()}</div>
        </div>
        <div className="bg-gray-800/50 rounded p-2">
          <div className="text-xs text-gray-500 mb-1">Best Price</div>
          <div className={`font-mono text-lg ${activeTab === 'sell' ? 'text-green-400' : 'text-blue-400'}`}>
            {bestPrice > 0 ? `${formatCredits(bestPrice)}cr` : '-'}
          </div>
        </div>
        <div className="bg-gray-800/50 rounded p-2">
          <div className="text-xs text-gray-500 mb-1">Orders</div>
          <div className="font-mono text-lg text-white">{orders.length}</div>
        </div>
        <div className="bg-gray-800/50 rounded p-2">
          <div className="text-xs text-gray-500 mb-1">Price Range</div>
          <div className={`font-mono text-sm ${activeTab === 'sell' ? 'text-green-400' : 'text-blue-400'}`}>
            {priceMin === priceMax
              ? `${formatCredits(priceMin)}cr`
              : `${formatCredits(priceMin)} - ${formatCredits(priceMax)}cr`}
          </div>
        </div>
      </div>

      {/* Both sides summary */}
      {activeTab === 'sell' && item.buy_orders?.length > 0 && (
        <div className="bg-blue-900/20 border border-blue-800/30 rounded p-2 mb-4 text-xs">
          <span className="text-gray-400">Best buy order: </span>
          <span className="text-blue-400 font-mono">{formatCredits(item.best_buy)}cr</span>
          <span className="text-gray-500 ml-1">({item.buy_orders.length} orders)</span>
        </div>
      )}
      {activeTab === 'buy' && item.sell_orders?.length > 0 && (
        <div className="bg-green-900/20 border border-green-800/30 rounded p-2 mb-4 text-xs">
          <span className="text-gray-400">Best sell order: </span>
          <span className="text-green-400 font-mono">{formatCredits(item.best_sell)}cr</span>
          <span className="text-gray-500 ml-1">({item.sell_orders.length} orders)</span>
        </div>
      )}

      {/* Order book */}
      <div className="text-xs text-gray-500 mb-2 uppercase">
        {activeTab === 'sell' ? 'Sell' : 'Buy'} Order Book
      </div>
      <div className="flex-1 overflow-y-auto">
        <table className="w-full text-xs">
          <thead>
            <tr className="text-gray-500 border-b border-gray-700">
              <th className="pb-1 text-left">Price</th>
              <th className="pb-1 text-right">Qty</th>
              <th className="pb-1 text-right">Total</th>
              {activeTab === 'sell' && <th className="pb-1 text-right">Buy</th>}
            </tr>
          </thead>
          <tbody>
            {sorted.map((order, idx) => {
              const selected = buyQtys[idx] ?? 0;
              return (
                <tr key={idx} className="border-b border-gray-800/50">
                  <td className={`py-1 font-mono ${activeTab === 'sell' ? 'text-green-400' : 'text-blue-400'}`}>
                    {formatCredits(order.price_each)}cr
                  </td>
                  <td className="py-1 text-right font-mono text-white">{order.quantity.toLocaleString()}</td>
                  <td className="py-1 text-right font-mono text-gray-400">
                    {formatCredits(order.quantity * order.price_each)}cr
                  </td>
                  {activeTab === 'sell' && (
                    <td className="py-1 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => adjustQty(idx, -1, order.quantity)}
                          disabled={selected <= 0}
                          className="w-5 h-5 flex items-center justify-center rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-30 disabled:cursor-not-allowed text-white text-xs"
                        >
                          -
                        </button>
                        <span className={`font-mono w-8 text-center ${selected > 0 ? 'text-amber-400' : 'text-gray-600'}`}>
                          {selected}
                        </span>
                        <button
                          onClick={() => adjustQty(idx, 1, order.quantity)}
                          disabled={selected >= order.quantity}
                          className="w-5 h-5 flex items-center justify-center rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-30 disabled:cursor-not-allowed text-white text-xs"
                        >
                          +
                        </button>
                      </div>
                    </td>
                  )}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* Buy summary and button */}
      {activeTab === 'sell' && (
        <div className="mt-3 pt-3 border-t border-gray-700">
          {totalBuyQty > 0 && (
            <div className="flex justify-between text-xs mb-2">
              <span className="text-gray-400">
                Buying <span className="text-white font-mono">{totalBuyQty.toLocaleString()}</span> units
              </span>
              <span className="text-gray-400">
                Cost: <span className="text-amber-400 font-mono">{formatCredits(totalBuyCost)}cr</span>
              </span>
            </div>
          )}
          <button
            onClick={handleBuy}
            disabled={totalBuyQty <= 0}
            className="w-full py-2 rounded text-sm font-medium transition-colors bg-cyan-700 hover:bg-cyan-600 text-white disabled:opacity-30 disabled:cursor-not-allowed"
          >
            BUY {totalBuyQty > 0 ? `${totalBuyQty.toLocaleString()} x ${formatItemName(item)}` : ''}
          </button>
        </div>
      )}
    </>
  );
}
