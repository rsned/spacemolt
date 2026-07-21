import { useCallback, useEffect, useState } from 'react';

export interface SystemPOI {
  id: string;
  name: string;
  type: string;
  class: string;
  x: number;
  y: number;
}

// Session-lifetime cache: POI layouts are static game topology.
const cache = new Map<string, SystemPOI[]>();

export function useSystemPois(systemId: string): {
  pois: SystemPOI[] | null; error: string | null; retry: () => void;
} {
  const [pois, setPois] = useState<SystemPOI[] | null>(cache.get(systemId) ?? null);
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const cached = cache.get(systemId);
    if (cached) { setPois(cached); setError(null); return; }
    let cancelled = false;
    setPois(null);
    setError(null);
    fetch(`/api/overmind/system/${encodeURIComponent(systemId)}/pois`)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<SystemPOI[]>;
      })
      .then((data) => {
        if (cancelled) return;
        cache.set(systemId, data);
        setPois(data);
      })
      .catch((err) => { if (!cancelled) setError(String(err)); });
    return () => { cancelled = true; };
  }, [systemId, attempt]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  return { pois, error, retry };
}
