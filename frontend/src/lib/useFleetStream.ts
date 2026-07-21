import { useEffect, useRef, useState } from 'react';

export interface AgentState {
  fleet: string;
  agent_id: string;
  role: string;
  system_id: string;
  system_name: string;
  poi: string;
  docked: boolean;
  credits: number;
  hull: number;
  max_hull: number;
  fuel: number;
  max_fuel: number;
  cargo_used: number;
  cargo_capacity: number;
  activity?: string;
  healthy: boolean;
  seen: boolean;
  restarts: number;
  last_seen: string;
}

export interface SourceEarnings { total: number; per_hour: number; count: number }

export interface Accounting {
  total_credits: number;
  agents: number;
  healthy: number;
  unseen: number;
  restarts: number;
  haul: SourceEarnings;
  freight: SourceEarnings;
  missions: SourceEarnings;
  combined_per_hour: number;
  per_agent_per_hour: number;
  oldest_capture: string;
}

export interface AgentMove { agent: AgentState; from_system_id: string }

interface Snapshot {
  agents: AgentState[];
  off_map: AgentState[];
  stale_fleets: string[] | null;
}

interface Delta {
  moved: AgentMove[] | null;
  updated: AgentState[] | null;
  joined: AgentState[] | null;
  left: string[] | null;
  stale_fleets: string[] | null;
}

/** Fleet label -> accent color; must mirror pkg/ovdash Fleets. */
export const FLEETS: Record<string, string> = {
  haul: '#d4a017',
  mission: '#22d3ee',
  craft: '#34d399',
  mb: '#a78bfa',
  assist: '#fb923c',
  shuttle: '#f472b6',
};

export interface FleetStream {
  agents: Map<string, AgentState>;
  offMap: AgentState[];
  accounting: Accounting | null;
  staleFleets: string[];
  /** Moves from the most recent delta — consumed by the map for animation. */
  moves: AgentMove[];
  connected: boolean;
}

export function useFleetStream(streamURL = '/api/overmind/stream'): FleetStream {
  const [state, setState] = useState<FleetStream>({
    agents: new Map(), offMap: [], accounting: null,
    staleFleets: [], moves: [], connected: false,
  });
  const agentsRef = useRef(new Map<string, AgentState>());

  useEffect(() => {
    const es = new EventSource(streamURL);

    es.addEventListener('snapshot', (e) => {
      const snap: Snapshot = JSON.parse((e as MessageEvent).data);
      const m = new Map<string, AgentState>();
      (snap.agents ?? []).forEach((a) => m.set(a.agent_id, a));
      agentsRef.current = m;
      setState((s) => ({
        ...s, agents: new Map(m), offMap: snap.off_map ?? [],
        staleFleets: snap.stale_fleets ?? [], moves: [], connected: true,
      }));
    });

    es.addEventListener('delta', (e) => {
      const d: Delta = JSON.parse((e as MessageEvent).data);
      const m = agentsRef.current;
      (d.joined ?? []).forEach((a) => m.set(a.agent_id, a));
      (d.updated ?? []).forEach((a) => m.set(a.agent_id, a));
      (d.moved ?? []).forEach(({ agent }) => m.set(agent.agent_id, agent));
      (d.left ?? []).forEach((id) => m.delete(id));
      setState((s) => ({
        ...s, agents: new Map(m),
        staleFleets: d.stale_fleets ?? s.staleFleets,
        moves: d.moved ?? [], connected: true,
      }));
    });

    es.addEventListener('accounting', (e) => {
      const acct: Accounting = JSON.parse((e as MessageEvent).data);
      setState((s) => ({ ...s, accounting: acct }));
    });

    es.onerror = () => setState((s) => ({ ...s, connected: false }));
    // EventSource auto-reconnects; the next snapshot keyframe repaints state.

    return () => es.close();
  }, [streamURL]);

  return state;
}
