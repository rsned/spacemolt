import { useEffect, useState } from 'react';

/** Mirrors pkg/assets RosterShip. */
export interface RosterShip {
  ship_id: string;
  class_id: string;
  class_name: string;
  hull_current: number;
  hull_max: number;
  fuel_current: number;
  fuel_max: number;
  cargo_used: number;
  cargo_capacity: number;
}

export interface RosterCapability {
  eligible: boolean;
  reason?: string;
}

/** Mirrors pkg/assets RosterRow. */
export interface RosterRow {
  player_id: string;
  agent_id: string;
  username: string;
  empire: string;
  credits: number;
  faction_id: string;
  faction_rank: string;
  current_system: string;
  current_poi: string;
  docked_at_base: string;
  experience: number;
  captured_at: string;
  ship?: RosterShip;
  capabilities: Record<string, RosterCapability>;
  fleet?: string;
  role?: string;
  stale: boolean;
}

export interface SheetSkill { skill: string; level: number; xp: number }
export interface SheetStanding {
  faction: string; reputation: number; baseline: number;
  outstanding_bounty: number; jailed_until?: string;
}
export interface SheetHull extends RosterShip {
  is_active: boolean; location: string; modules: number; listing_price?: number;
}
export interface SheetStorage { base_id: string; credits: number; items: number; units: number }

/** Mirrors pkg/assets Sheet. */
export interface AgentSheetData extends RosterRow {
  skills: SheetSkill[] | null;
  standings: SheetStanding[] | null;
  hulls: SheetHull[] | null;
  storage: SheetStorage[] | null;
}

interface Fetched<T> { data: T | null; error: string | null; loading: boolean }

function useFetch<T>(url: string): Fetched<T> {
  const [state, setState] = useState<Fetched<T>>({ data: null, error: null, loading: true });
  useEffect(() => {
    let alive = true;
    setState({ data: null, error: null, loading: true });
    fetch(url)
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        return res.json() as Promise<T>;
      })
      .then((data) => { if (alive) setState({ data, error: null, loading: false }); })
      .catch((err) => { if (alive) setState({ data: null, error: String(err), loading: false }); });
    return () => { alive = false; };
  }, [url]);
  return state;
}

export function useRoster(): Fetched<RosterRow[]> {
  return useFetch<RosterRow[]>('/api/overmind/roster');
}

export function useAgentSheet(agentId: string): Fetched<AgentSheetData> {
  return useFetch<AgentSheetData>(`/api/overmind/agents/${encodeURIComponent(agentId)}/sheet`);
}
