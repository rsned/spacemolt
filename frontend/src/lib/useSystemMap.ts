import { useEffect, useRef, useState } from 'react';
import type { POI, JumpGate } from '../types/game';

interface APISystemDetail {
  system: {
    id: string;
    name: string;
    position: { x: number; y: number };
    police_level: number;
    faction: string;
    connections: string[];
  };
  pois: {
    id: string;
    name: string;
    type: string;
    position: { x: number; y: number };
    resources: { resource_id: string; richness: number; remaining: number }[];
  }[];
  connections: {
    id: string;
    name: string;
    position: { x: number; y: number };
  }[];
}

export interface SystemMapData {
  pois: POI[];
  jumpGates: JumpGate[];
}

function computeAngle(from: { x: number; y: number }, to: { x: number; y: number }): number {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  // atan2(dx, dy) gives angle from +Y axis (North) clockwise.
  // Negate dy because the system map flips Y (screen Y increases downward).
  const rad = Math.atan2(dx, -dy);
  let deg = rad * (180 / Math.PI);
  if (deg < 0) deg += 360;
  return deg;
}

function mapPOIs(apiPOIs: APISystemDetail['pois']): POI[] {
  return apiPOIs.map((p) => ({
    id: p.id,
    name: p.name,
    type: p.type as POI['type'],
    x: p.position.x,
    y: p.position.y,
    resources: p.resources?.map((r) => ({
      name: r.resource_id,
      amount: r.remaining >= 0 ? r.remaining : r.richness,
    })),
  }));
}

function mapJumpGates(
  systemPos: { x: number; y: number },
  connections: APISystemDetail['connections'],
): JumpGate[] {
  return connections.map((c) => ({
    id: c.id,
    name: c.name,
    angle: computeAngle(systemPos, c.position),
  }));
}

export function useSystemMap(systemId: string | undefined): SystemMapData | null {
  const [data, setData] = useState<SystemMapData | null>(null);
  const prevId = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!systemId) {
      setData(null);
      return;
    }

    // Don't re-fetch if the system hasn't changed.
    if (systemId === prevId.current) return;
    prevId.current = systemId;

    let cancelled = false;

    fetch(`/api/systems/${encodeURIComponent(systemId)}`)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<APISystemDetail>;
      })
      .then((detail) => {
        if (cancelled) return;
        setData({
          pois: mapPOIs(detail.pois),
          jumpGates: mapJumpGates(detail.system.position, detail.connections),
        });
      })
      .catch((err) => {
        if (!cancelled) {
          console.error('Failed to fetch system map data:', err);
          setData(null);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [systemId]);

  return data;
}
