import { useCallback, useEffect, useRef, useState } from 'react';
import type { Player, Skill } from '../types/game';

export interface AgentInfo {
  username: string;
  connected: boolean;
  system?: string;
  poi?: string;
  docked: boolean;
}

interface ServerMessage {
  type: string;
  agent?: string;
  message?: unknown;
  agents?: AgentInfo[];
  connected?: boolean;
  error?: string;
}

interface GameState {
  Username: string;
  CurrentSystem: string;
  CurrentPOI: string;
  Doc: boolean;
  Traveling: boolean;
  Credits: number;
  CurrentTick: number;
  Hull: number;
  MaxHull: number;
  Fuel: number;
  MaxFuel: number;
  InCombat: boolean;
  Player: {
    username: string;
    empire: string;
    credits: number;
    current_system: string;
    current_poi: string;
    skills: Record<string, { level: number; xp: number }>;
  };
  Ship: {
    name: string;
    class_id: string;
    hull: number;
    max_hull: number;
    shield: number;
    max_shield: number;
    fuel: number;
    max_fuel: number;
    cargo_used: number;
    cargo_capacity: number;
  };
  System: {
    id: string;
    name: string;
    empire: string;
    police_level: number;
    security_status: string;
  };
  SkillNextLevelXP: Record<string, number>;
}

// Map raw protocol.Response payload to a partial GameState update.
// The game server sends different shapes depending on message type.
// Note: protocol.Response JSON uses lowercase keys (type, payload).
function extractGameState(msg: { type: string; payload: Record<string, unknown> }): Partial<GameState> | null {
  const p = msg.payload;
  if (!p) return null;

  const state: Partial<GameState> = {};
  let hasData = false;

  if (p.player && typeof p.player === 'object') {
    state.Player = p.player as GameState['Player'];
    hasData = true;
  }
  if (p.ship && typeof p.ship === 'object') {
    state.Ship = p.ship as GameState['Ship'];
    hasData = true;
  }
  if (p.system && typeof p.system === 'object') {
    state.System = p.system as GameState['System'];
    hasData = true;
  }
  if (typeof p.tick === 'number') {
    state.CurrentTick = p.tick;
    hasData = true;
  }
  // get_skills response includes player_skills with next_level_xp per skill.
  if (Array.isArray(p.player_skills)) {
    const nextXP: Record<string, number> = {};
    for (const entry of p.player_skills) {
      const e = entry as Record<string, unknown>;
      if (typeof e.skill_id === 'string' && typeof e.next_level_xp === 'number') {
        nextXP[e.skill_id] = e.next_level_xp;
      }
    }
    if (Object.keys(nextXP).length > 0) {
      state.SkillNextLevelXP = nextXP;
      hasData = true;
    }
  }

  return hasData ? state : null;
}

function mapToPlayer(gs: GameState): Player {
  const empire = (gs.Player?.empire || 'outerrim').toLowerCase();
  const policeLevel = gs.System?.police_level ?? 0;

  return {
    username: gs.Player?.username || gs.Username || 'Unknown',
    ship: gs.Ship?.name || 'Unknown Ship',
    shipClass: gs.Ship?.class_id || 'UNKNOWN',
    empire: empire as Player['empire'],
    credits: gs.Player?.credits ?? gs.Credits ?? 0,
    hull: gs.Ship?.hull ?? gs.Hull ?? 0,
    hullMax: gs.Ship?.max_hull ?? gs.MaxHull ?? 100,
    shield: gs.Ship?.shield ?? 0,
    shieldMax: gs.Ship?.max_shield ?? 0,
    fuel: gs.Ship?.fuel ?? gs.Fuel ?? 0,
    fuelMax: gs.Ship?.max_fuel ?? gs.MaxFuel ?? 100,
    cargo: gs.Ship?.cargo_used ?? 0,
    cargoMax: gs.Ship?.cargo_capacity ?? 0,
    location: {
      systemId: gs.System?.id || '',
      system: gs.System?.name || gs.CurrentSystem || 'Unknown',
      poi: gs.CurrentPOI || gs.Player?.current_poi || 'Unknown',
      dockedAt: gs.Doc ? (gs.CurrentPOI || null) : null,
    },
    policeLevel: policeLevel > 0 ? 'policed' : 'lawless',
    tick: gs.CurrentTick ?? 0,
  };
}

function mapToSkills(gs: GameState): Skill[] {
  const skills = gs.Player?.skills;
  if (!skills) return [];

  const nextLevelXP = gs.SkillNextLevelXP || {};

  return Object.entries(skills).map(([name, skill]) => {
    const nextXP = nextLevelXP[name];
    let xpPct: number;
    if (nextXP && nextXP > 0) {
      xpPct = (skill.xp / nextXP) * 100;
    } else {
      // No next-level data yet; show raw xp as-is (capped at 99 to indicate incomplete).
      xpPct = Math.min(skill.xp > 0 ? skill.xp : 0, 99);
    }
    return {
      name,
      level: skill.level,
      xp: Math.round(xpPct),
      nextLevelXp: nextXP ?? 0,
    };
  }).sort((a, b) => b.level - a.level || a.name.localeCompare(b.name));
}

export type ConnectionStatus = 'disconnected' | 'connecting' | 'connected';

export interface ObserverState {
  status: ConnectionStatus;
  player: Player | null;
  skills: Skill[];
  agents: AgentInfo[];
  subscribedAgent: string | null;
  error: string | null;
}

export function useObserver(wsUrl: string) {
  const wsRef = useRef<WebSocket | null>(null);
  const gameStateRef = useRef<Partial<GameState>>({});

  const [state, setState] = useState<ObserverState>({
    status: 'disconnected',
    player: null,
    skills: [],
    agents: [],
    subscribedAgent: null,
    error: null,
  });

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) return;

    setState(s => ({ ...s, status: 'connecting', error: null }));

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setState(s => ({ ...s, status: 'connected' }));
      // Request agent list on connect.
      ws.send(JSON.stringify({ type: 'list_agents' }));
    };

    ws.onmessage = (evt) => {
      try {
        const msg = JSON.parse(evt.data) as ServerMessage;
        handleMessage(msg);
      } catch {
        // Ignore unparseable messages.
      }
    };

    ws.onclose = () => {
      wsRef.current = null;
      setState(s => ({ ...s, status: 'disconnected' }));
    };

    ws.onerror = () => {
      setState(s => ({ ...s, error: 'WebSocket connection error' }));
    };
  }, [wsUrl]);

  const disconnect = useCallback(() => {
    wsRef.current?.close();
    wsRef.current = null;
    gameStateRef.current = {};
    setState({
      status: 'disconnected',
      player: null,
      skills: [],
      agents: [],
      subscribedAgent: null,
      error: null,
    });
  }, []);

  const subscribe = useCallback((agentName: string) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;

    gameStateRef.current = {};
    setState(s => ({ ...s, subscribedAgent: agentName, player: null, skills: [] }));
    ws.send(JSON.stringify({ type: 'subscribe', agent: agentName }));
  }, []);

  const addAgent = useCallback(async (username: string): Promise<boolean> => {
    setState(s => ({ ...s, error: null }));
    try {
      const resp = await fetch('/api/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username }),
      });
      if (!resp.ok) {
        const err = await resp.json();
        setState(s => ({ ...s, error: err.error || 'Failed to add agent' }));
        return false;
      }
      // Refresh agent list.
      wsRef.current?.send(JSON.stringify({ type: 'list_agents' }));
      return true;
    } catch (err) {
      setState(s => ({ ...s, error: `Failed to add agent: ${err}` }));
      return false;
    }
  }, [wsUrl]);

  const removeAgent = useCallback(async (username: string): Promise<boolean> => {
    setState(s => ({ ...s, error: null }));
    try {
      const resp = await fetch(`/api/agents/${encodeURIComponent(username)}`, {
        method: 'DELETE',
      });
      if (!resp.ok) {
        const err = await resp.json();
        setState(s => ({ ...s, error: err.error || 'Failed to remove agent' }));
        return false;
      }
      // Clear player state if we were watching this agent.
      setState(s => {
        if (s.subscribedAgent === username) {
          return { ...s, subscribedAgent: null, player: null, skills: [] };
        }
        return s;
      });
      gameStateRef.current = {};
      // Refresh agent list.
      wsRef.current?.send(JSON.stringify({ type: 'list_agents' }));
      return true;
    } catch (err) {
      setState(s => ({ ...s, error: `Failed to remove agent: ${err}` }));
      return false;
    }
  }, []);

  const listAgents = useCallback(() => {
    wsRef.current?.send(JSON.stringify({ type: 'list_agents' }));
  }, []);

  function handleMessage(msg: ServerMessage) {
    switch (msg.type) {
      case 'agent_list':
        setState(s => ({ ...s, agents: msg.agents || [] }));
        break;

      case 'game_message': {
        // msg.message could be a full State snapshot (from subscribe) or a protocol.Response.
        // protocol.Response uses lowercase JSON keys: {"type": "...", "payload": {...}}
        // game.State uses Go-default uppercase keys: {"Username": "...", "Ship": {...}}
        const raw = msg.message as Record<string, unknown>;
        if (!raw) break;

        // Check for protocol.Response (lowercase "type" and "payload" from JSON tags).
        const respType = raw.type as string | undefined;
        const respPayload = raw.payload as Record<string, unknown> | undefined;

        if (respType && respPayload) {
          const partial = extractGameState({ type: respType, payload: respPayload });
          if (partial) {
            gameStateRef.current = deepMerge(gameStateRef.current, partial);
            const gs = gameStateRef.current as GameState;
            setState(s => ({
              ...s,
              player: mapToPlayer(gs),
              skills: mapToSkills(gs),
            }));
          }
        } else {
          // Full state snapshot (e.g. from initial subscribe) — Go struct with uppercase keys.
          gameStateRef.current = deepMerge(gameStateRef.current, raw as Partial<GameState>);
          const gs = gameStateRef.current as GameState;
          setState(s => ({
            ...s,
            player: mapToPlayer(gs),
            skills: mapToSkills(gs),
          }));
        }
        break;
      }

      case 'agent_status':
        // Update agent list entry.
        setState(s => ({
          ...s,
          agents: s.agents.map(a =>
            a.username === msg.agent
              ? { ...a, connected: msg.connected ?? a.connected }
              : a
          ),
        }));
        break;

      case 'error':
        setState(s => ({ ...s, error: msg.error || 'Unknown error' }));
        break;
    }
  }

  useEffect(() => {
    return () => {
      wsRef.current?.close();
    };
  }, []);

  return {
    ...state,
    connect,
    disconnect,
    subscribe,
    addAgent,
    removeAgent,
    listAgents,
  };
}

function deepMerge<T extends Record<string, unknown>>(target: Partial<T>, source: Partial<T>): Partial<T> {
  const result = { ...target } as Record<string, unknown>;
  for (const key of Object.keys(source)) {
    const srcVal = (source as Record<string, unknown>)[key];
    const tgtVal = result[key];
    if (
      srcVal && typeof srcVal === 'object' && !Array.isArray(srcVal) &&
      tgtVal && typeof tgtVal === 'object' && !Array.isArray(tgtVal)
    ) {
      result[key] = deepMerge(
        tgtVal as Record<string, unknown>,
        srcVal as Record<string, unknown>,
      );
    } else {
      result[key] = srcVal;
    }
  }
  return result as Partial<T>;
}
