import { useState, useEffect } from 'react';
import type { Base } from '../types/game';

interface StationDataResponse {
  base: Base;
}

export function useStationData(poiId: string | null) {
  const [data, setData] = useState<Base | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!poiId) {
      setData(null);
      setError(null);
      return;
    }

    setLoading(true);
    setError(null);

    fetch(`/api/pois/${poiId}/base`)
      .then((res) => {
        if (!res.ok) {
          throw new Error(`Failed to fetch station data: ${res.statusText}`);
        }
        return res.json();
      })
      .then((response: StationDataResponse) => {
        setData(response.base);
      })
      .catch((err) => {
        console.error('Error fetching station data:', err);
        setError(err.message);
      })
      .finally(() => {
        setLoading(false);
      });
  }, [poiId]);

  return { data, loading, error };
}
