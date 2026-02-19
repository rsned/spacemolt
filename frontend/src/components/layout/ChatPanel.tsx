import type { ChatMessage } from '../../types/game';
import { useState } from 'react';

interface ChatPanelProps {
  chat: ChatMessage[];
  isConnected: boolean;
}

export const ChatPanel: React.FC<ChatPanelProps> = ({ chat, isConnected }) => {
  const [activeTab, setActiveTab] = useState(0);
  const tabs = ['System', 'Local', 'Faction'];

  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <div className="flex gap-2 mb-3 text-xs">
        {tabs.map((tab, idx) => (
          <button
            key={tab}
            onClick={() => setActiveTab(idx)}
            className={`px-3 py-1 rounded ${
              idx === activeTab
                ? 'bg-cyan-600 text-white'
                : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>
      {!isConnected ? (
        <div className="flex flex-col items-center justify-center h-64 text-center">
          <svg className="w-12 h-12 text-gray-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
          </svg>
          <p className="text-gray-400 text-sm">Connect to an agent to view chat</p>
        </div>
      ) : (
        <>
          <div className="space-y-2 mb-3 max-h-48 overflow-y-auto scrollbar-thin text-sm">
            {chat.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-48 text-center">
                <svg className="w-12 h-12 text-gray-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
                </svg>
                <p className="text-gray-400 text-sm">No chat messages yet</p>
              </div>
            ) : (
              chat.map((msg, idx) => (
                <div key={idx} className={msg.type === 'system' ? 'text-cyan-400 italic' : ''}>
                  {msg.type === 'system' ? (
                    <span>─ System: {msg.message} ─</span>
                  ) : (
                    <>
                      <span className="text-gray-500">[{msg.time}]</span>{' '}
                      <span className="text-cyan-300">{msg.user}:</span>{' '}
                      <span className="text-gray-300">{msg.message}</span>
                    </>
                  )}
                </div>
              ))
            )}
          </div>
          <div className="flex gap-2">
            <input
              type="text"
              placeholder="Type message..."
              className="flex-1 bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm focus:outline-none focus:border-cyan-500"
            />
            <button className="bg-cyan-600 hover:bg-cyan-500 px-4 py-2 rounded text-sm">
              Send
            </button>
          </div>
        </>
      )}
    </div>
  );
};
