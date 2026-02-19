import type { Notification } from '../../types/game';

interface NotificationFeedProps {
  notifications: Notification[];
  isConnected: boolean;
}

export const NotificationFeed: React.FC<NotificationFeedProps> = ({ notifications, isConnected }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 text-sm mb-3">NOTIFICATIONS</h3>
      {!isConnected ? (
        <div className="flex flex-col items-center justify-center h-64 text-center">
          <svg className="w-12 h-12 text-gray-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
          </svg>
          <p className="text-gray-400 text-sm">Connect to an agent to view notifications</p>
        </div>
      ) : notifications.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-64 text-center">
          <svg className="w-12 h-12 text-gray-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4" />
          </svg>
          <p className="text-gray-400 text-sm">No notifications yet</p>
        </div>
      ) : (
        <div className="space-y-2 max-h-64 overflow-y-auto scrollbar-thin">
          {notifications.map((notif, idx) => (
            <div
              key={idx}
              className="flex items-start gap-2 p-2 rounded bg-gray-800/50 hover:bg-gray-800 transition-colors"
            >
              <span className="text-lg">{notif.icon}</span>
              <div className="flex-1">
                <span className="text-sm text-gray-300">{notif.message}</span>
                <span className="text-xs text-gray-500 ml-2">({notif.time})</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
