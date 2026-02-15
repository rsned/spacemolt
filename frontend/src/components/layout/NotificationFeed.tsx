import type { Notification } from '../../types/game';

interface NotificationFeedProps {
  notifications: Notification[];
}

export const NotificationFeed: React.FC<NotificationFeedProps> = ({ notifications }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 text-sm mb-3">NOTIFICATIONS</h3>
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
    </div>
  );
};
