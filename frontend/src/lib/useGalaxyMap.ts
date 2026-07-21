import { useEffect, useState } from 'react';

export interface GalaxySystem {
  id: string;
  name: string;
  position: { x: number; y: number };
  police_level: number;
  last_visited_tick: number;
  empire: string;
  is_stronghold: boolean;
  connections: string[];
}

export interface AgentLocation {
  username: string;
  systemId: string;
  poi: string;
  docked: boolean;
}

export interface GalaxyMapData {
  systems: GalaxySystem[];
  agentLocations: AgentLocation[];
}

export function useGalaxyMap(basePath = ''): GalaxyMapData | null {
  const [data, setData] = useState<GalaxyMapData | null>(null);

  useEffect(() => {
    let cancelled = false;

    const systemsURL = basePath ? `${basePath}/systems` : '/api/systems';

    // Fetch all systems
    fetch(systemsURL)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<GalaxySystem[]>;
      })
      .then((systems) => {
        if (cancelled) return;

        // When basePath is set the caller owns agent data (SSE); skip /api/agents.
        if (basePath) {
          setData({ systems, agentLocations: [] });
          return;
        }

        // Fetch agents to get their locations
        return fetch('/api/agents')
          .then((res) => {
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            return res.json() as Promise<Array<{
              username: string;
              connected: boolean;
              system?: string;
              poi?: string;
              docked: boolean;
            }>>;
          })
          .then((agents) => {
            if (cancelled) return;

            // Map agents to their locations
            const agentLocations: AgentLocation[] = agents
              .filter((agent) => agent.system) // Only agents with known locations
              .map((agent) => ({
                username: agent.username,
                systemId: agent.system || '',
                poi: agent.poi || '',
                docked: agent.docked,
              }));

            setData({ systems, agentLocations });
          });
      })
      .catch((err) => {
        if (!cancelled) {
          console.error('Failed to fetch galaxy map data:', err);
          setData(null);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [basePath]);

  return data;
}
