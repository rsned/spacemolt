import { useState } from 'react';
import type { useObserver } from '../../lib/useObserver';

interface ConnectionPanelProps {
  observer: ReturnType<typeof useObserver>;
}

export const ConnectionPanel: React.FC<ConnectionPanelProps> = ({ observer }) => {
  const [agentInput, setAgentInput] = useState('');
  const [adding, setAdding] = useState(false);

  const handleConnect = () => {
    if (observer.status === 'disconnected') {
      observer.connect();
    } else {
      observer.disconnect();
    }
  };

  const handleAddAgent = async () => {
    if (!agentInput.trim()) return;
    setAdding(true);
    const ok = await observer.addAgent(agentInput.trim());
    if (ok) {
      observer.subscribe(agentInput.trim());
      setAgentInput('');
    }
    setAdding(false);
  };

  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <div className="flex items-center gap-4">
        {/* Connect/Disconnect */}
        <button
          onClick={handleConnect}
          className={`px-4 py-2 rounded text-sm font-medium transition-colors ${
            observer.status === 'disconnected'
              ? 'bg-cyan-600 hover:bg-cyan-500 text-white'
              : 'bg-red-800 hover:bg-red-700 text-red-200'
          }`}
        >
          {observer.status === 'disconnected' ? 'Connect to Server' :
           observer.status === 'connecting' ? 'Connecting...' : 'Disconnect'}
        </button>

        {/* Add Agent */}
        {observer.status === 'connected' && (
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={agentInput}
              onChange={(e) => setAgentInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleAddAgent()}
              placeholder="Agent username"
              className="bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:border-cyan-500 focus:outline-none"
            />
            <button
              onClick={handleAddAgent}
              disabled={adding || !agentInput.trim()}
              className="px-4 py-2 rounded text-sm font-medium bg-green-800 hover:bg-green-700 text-green-200 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {adding ? 'Adding...' : 'Add & Watch'}
            </button>
          </div>
        )}

        {/* Agent List */}
        {observer.agents.length > 0 && (
          <div className="flex items-center gap-2 ml-4">
            <span className="text-xs text-gray-500">Agents:</span>
            {observer.agents.map((agent) => (
              <button
                key={agent.username}
                onClick={() => observer.subscribe(agent.username)}
                className={`px-3 py-1 rounded text-xs transition-colors ${
                  observer.subscribedAgent === agent.username
                    ? 'bg-cyan-700 text-cyan-200'
                    : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
                }`}
              >
                <span className={`inline-block w-1.5 h-1.5 rounded-full mr-1.5 ${
                  agent.connected ? 'bg-green-400' : 'bg-red-400'
                }`} />
                {agent.username}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Error display */}
      {observer.error && (
        <div className="mt-2 text-sm text-red-400 bg-red-900/30 rounded px-3 py-1">
          {observer.error}
        </div>
      )}
    </div>
  );
};
