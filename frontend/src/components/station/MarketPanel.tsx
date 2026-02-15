import type { Player, MarketOrder } from '../../types/game';
import { useState } from 'react';

interface MarketPanelProps {
  player: Player;
  orders: MarketOrder[];
}

export const MarketPanel: React.FC<MarketPanelProps> = ({ player, orders }) => {
  const [activeTab, setActiveTab] = useState(1); // 0: Buy, 1: Sell, 2: My Orders

  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 mb-4">EXCHANGE</h3>

      <div className="flex gap-2 mb-4">
        {['BUY', 'SELL', 'MY ORDERS'].map((tab, idx) => (
          <button
            key={tab}
            onClick={() => setActiveTab(idx)}
            className={`px-4 py-2 rounded ${
              idx === activeTab ? 'bg-cyan-600 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      {activeTab === 1 && (
        <>
          <div className="text-sm text-gray-400 mb-2">CARGO ITEMS:</div>
          <table className="w-full text-sm mb-4">
            <thead>
              <tr className="text-left text-gray-500 border-b border-gray-700">
                <th className="pb-2">Item</th>
                <th className="pb-2">Qty</th>
                <th className="pb-2">Best Bid</th>
                <th className="pb-2">Value</th>
                <th className="pb-2">Action</th>
              </tr>
            </thead>
            <tbody>
              {orders.map((order, idx) => (
                <tr key={idx} className="border-b border-gray-800">
                  <td className="py-2 font-mono text-cyan-300">{order.item}</td>
                  <td className="py-2">{order.quantity}</td>
                  <td className="py-2">{order.bestBid}cr</td>
                  <td className="py-2">{order.value}cr</td>
                  <td className="py-2">
                    <button className="bg-green-700 hover:bg-green-600 px-3 py-1 rounded text-xs">
                      Sell
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>

          <div className="flex gap-4 items-center">
            <span className="text-sm text-gray-400">Sell Qty:</span>
            <input
              type="number"
              defaultValue="45"
              className="bg-gray-800 border border-gray-700 rounded px-3 py-1 w-24 text-sm"
            />
            <span className="text-sm text-gray-400">at</span>
            <select className="bg-gray-800 border border-gray-700 rounded px-3 py-1 text-sm">
              <option>market</option>
            </select>
            <button className="bg-cyan-600 hover:bg-cyan-500 px-4 py-1 rounded text-sm">
              CONFIRM
            </button>
          </div>
        </>
      )}

      <div className="mt-4 pt-4 border-t border-gray-700 flex justify-between text-sm">
        <span className="text-gray-400">
          Credits: <span className="text-green-400 font-mono">{player.credits.toLocaleString()}cr</span>
        </span>
        <span className="text-gray-400">
          Cargo: <span className="text-amber-400 font-mono">{player.cargo}/{player.cargoMax}</span>
        </span>
      </div>
    </div>
  );
};
