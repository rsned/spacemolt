import { useEffect, useState } from 'react';

interface SystemDetails {
  policeLevel: number;
  lastVisitedTick: number;
  loading: boolean;
}

/**
 * Fetches system details from the knowledge base API.
 * This provides accurate police levels since the game server's
 * WebSocket messages always send police_level: 0.
 */
export function useSystemDetails(systemId: string | null): SystemDetails {
  const [policeLevel, setPoliceLevel] = useState(0);
  const [lastVisitedTick, setLastVisitedTick] = useState(0);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!systemId) {
      setPoliceLevel(0);
      setLastVisitedTick(0);
      return;
    }

    setLoading(true);

    fetch(`/api/systems/${encodeURIComponent(systemId)}`)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => {
        setPoliceLevel(data.system?.police_level ?? 0);
        setLastVisitedTick(data.system?.last_visited_tick ?? 0);
      })
      .catch((err) => {
        console.error('Failed to fetch system details:', err);
        setPoliceLevel(0);
        setLastVisitedTick(0);
      })
      .finally(() => {
        setLoading(false);
      });
  }, [systemId]);

  return { policeLevel, lastVisitedTick, loading };
}
