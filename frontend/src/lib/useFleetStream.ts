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
  leaving?: boolean;
  version?: string;
  commit?: string;
  built_at?: string;
  code_dirty?: boolean;
  modified?: boolean;
  tier?: Tier;
}

export type Tier = 'green' | 'yellow' | 'red';

/** Tier → badge color. Kept distinct from FLEETS accent colors. */
export const TIER_COLORS: Record<Tier, string> = {
  green: '#34d399',
  yellow: '#fbbf24',
  red: '#f87171',
};

export interface OvermindInfo {
  version?: string;
  commit?: string;
  built_at?: string;
  code_dirty?: boolean;
  modified?: boolean;
  tier?: Tier;
  fleet_tier?: Tier;
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

export interface AssetCoverageRow {
  source: string;
  agents: number;
  oldest: string;
  stale: number;
}

interface Snapshot {
  agents: AgentState[] | null;
  off_map: AgentState[] | null;
  stale_fleets: string[] | null;
  removed?: Record<string, string[]>;
  overminds?: Record<string, OvermindInfo>;
  current_overmind?: string;
  current_worker?: string;
  asset_coverage?: AssetCoverageRow[];
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
  /** Fleet label -> override-removed agent ids. Only carried on the
   * "snapshot" event (full keyframe); deltas never touch it, so it persists
   * across delta updates until the next snapshot. */
  removed: Record<string, string[]>;
  /** Fleet label -> overmind build identity. Carried only on the "snapshot"
   * keyframe (like `removed`); persists across deltas until the next snapshot. */
  overminds: Record<string, OvermindInfo>;
  /** Newest build seen for each binary. The two roll out independently, so
   * they are reported separately rather than as one "current". Snapshot-only,
   * like `overminds`. */
  currentOvermind: string;
  currentWorker: string;
  /** Per-source freshness of the agent asset ledger. Snapshot-only, like
   * `overminds`: coverage only ever arrives on a full snapshot, and persists
   * across deltas until the next one. Empty when the ledger isn't deployed. */
  assetCoverage: AssetCoverageRow[];
  /** Moves from the most recent delta — consumed by the map for animation. */
  moves: AgentMove[];
  connected: boolean;
}

/** An agent with no resolved system belongs in the off-map tray, not the map. */
function isOffMap(a: AgentState): boolean {
  return !a.system_id;
}

/** Moves `a` into the collection matching its current system_id, removing it
 * from the other one first so an agent can never appear in both. */
function route(onMap: Map<string, AgentState>, offMap: Map<string, AgentState>, a: AgentState): void {
  onMap.delete(a.agent_id);
  offMap.delete(a.agent_id);
  (isOffMap(a) ? offMap : onMap).set(a.agent_id, a);
}

export function useFleetStream(streamURL = '/api/overmind/stream'): FleetStream {
  const [state, setState] = useState<FleetStream>({
    agents: new Map(), offMap: [], accounting: null,
    staleFleets: [], removed: {}, overminds: {},
    currentOvermind: '', currentWorker: '', assetCoverage: [], moves: [], connected: false,
  });
  const agentsRef = useRef(new Map<string, AgentState>());
  const offMapRef = useRef(new Map<string, AgentState>());

  useEffect(() => {
    const es = new EventSource(streamURL);

    es.addEventListener('snapshot', (e) => {
      const snap: Snapshot = JSON.parse((e as MessageEvent).data);
      const onMap = new Map<string, AgentState>();
      const offMap = new Map<string, AgentState>();
      [...(snap.agents ?? []), ...(snap.off_map ?? [])].forEach((a) => route(onMap, offMap, a));
      agentsRef.current = onMap;
      offMapRef.current = offMap;
      setState((s) => ({
        ...s, agents: new Map(onMap), offMap: [...offMap.values()],
        staleFleets: snap.stale_fleets ?? [], removed: snap.removed ?? {},
        overminds: snap.overminds ?? {},
        currentOvermind: snap.current_overmind ?? '',
        currentWorker: snap.current_worker ?? '',
        assetCoverage: snap.asset_coverage ?? [],
        moves: [], connected: true,
      }));
    });

    es.addEventListener('delta', (e) => {
      const d: Delta = JSON.parse((e as MessageEvent).data);
      const onMap = agentsRef.current;
      const offMap = offMapRef.current;
      (d.joined ?? []).forEach((a) => route(onMap, offMap, a));
      (d.updated ?? []).forEach((a) => route(onMap, offMap, a));
      (d.moved ?? []).forEach(({ agent }) => route(onMap, offMap, agent));
      (d.left ?? []).forEach((id) => { onMap.delete(id); offMap.delete(id); });
      setState((s) => ({
        ...s, agents: new Map(onMap), offMap: [...offMap.values()],
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
