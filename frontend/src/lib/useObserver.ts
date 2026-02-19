import { useCallback, useEffect, useRef, useState } from 'react';
import type { Player, Skill } from '../types/game';
import { useSkillDefinitions, getNextLevelXP } from './useSkillDefinitions';

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
  TravelProgress: number;
  TravelDestination: string;
  TravelType: 'travel' | 'jump';
  TravelArrivalTick: number;
  TravelStartTick: number;
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
    const playerObj = p.player as Record<string, unknown>;
    if ('docked_at_base' in playerObj) {
      state.Doc = typeof playerObj.docked_at_base === 'string' && playerObj.docked_at_base !== '';
    }
    hasData = true;
  }
  if (p.ship && typeof p.ship === 'object') {
    state.Ship = p.ship as GameState['Ship'];
    hasData = true;
  }

  // System object may be present in some responses
  if (p.system && typeof p.system === 'object') {
    state.System = p.system as GameState['System'];
    hasData = true;
  }

  // CurrentSystem is sent as a string (e.g., "sol") in most responses
  if (typeof p.current_system === 'string') {
    // Create a minimal System object from the string
    state.System = {
      id: p.current_system,
      name: p.current_system,
      empire: '',
      police_level: 0,
    } as GameState['System'];
    hasData = true;
  }

  if (typeof p.tick === 'number') {
    state.CurrentTick = p.tick;
    hasData = true;
  }

  // Extract current_poi from state_update payloads
  if (typeof p.current_poi === 'string') {
    state.CurrentPOI = p.current_poi;
    hasData = true;
  }

  // Track traveling state from state_update payloads
  if (typeof p.traveling === 'boolean') {
    state.Traveling = p.traveling;
    hasData = true;
  }
  // travel_progress fields from state_update
  if (typeof p.travel_progress === 'number') {
    state.Traveling = true;
    state.TravelProgress = p.travel_progress;
    hasData = true;
  }
  if (typeof p.travel_destination === 'string') {
    state.TravelDestination = p.travel_destination;
    hasData = true;
  }
  if (typeof p.travel_type === 'string') {
    state.TravelType = p.travel_type as 'travel' | 'jump';
    hasData = true;
  }
  if (typeof p.travel_arrival_tick === 'number') {
    state.TravelArrivalTick = p.travel_arrival_tick;
    hasData = true;
  }

  // Handle "ok" response with pending travel/jump command:
  // { command: "travel"|"jump", pending: true, message: "..." }
  if (p.pending === true && typeof p.command === 'string') {
    if (p.command === 'travel' || p.command === 'jump') {
      state.Traveling = true;
      state.TravelType = p.command as 'travel' | 'jump';
      // Record the start tick so we can interpolate
      if (typeof p.tick === 'number') {
        state.TravelStartTick = p.tick;
      }
      hasData = true;
    }
  }

  // Handle action_result for travel/jump completion:
  // { command: "travel"|"jump", result: { action: "arrived", poi_id: "...", poi: "..." } }
  if (p.result && typeof p.result === 'object') {
    const result = p.result as Record<string, unknown>;
    const command = p.command as string | undefined;
    if (result.action === 'undock') {
      state.Doc = false;
      hasData = true;
    }
    if (result.action === 'refuel') {
      if (!state.Ship) state.Ship = {} as GameState['Ship'];
      if (typeof result.fuel_now === 'number') state.Ship.fuel = result.fuel_now;
      if (typeof result.fuel_max === 'number') state.Ship.max_fuel = result.fuel_max;
      hasData = true;
    }
    if (result.action === 'repair') {
      if (!state.Ship) state.Ship = {} as GameState['Ship'];
      if (typeof result.hull_now === 'number') state.Ship.hull = result.hull_now;
      if (typeof result.hull_max === 'number') state.Ship.max_hull = result.hull_max;
      hasData = true;
    }
    if (result.action === 'arrived') {
      state.Traveling = false;
      state.TravelProgress = 0;
      state.TravelDestination = '';
      state.TravelArrivalTick = 0;
      state.TravelStartTick = 0;
      if (typeof result.poi_id === 'string') {
        state.CurrentPOI = result.poi_id;
      } else if (typeof result.poi === 'string') {
        state.CurrentPOI = result.poi;
      }
      // Jump arrival may include a new system
      if (command === 'jump' && typeof result.system_id === 'string') {
        state.System = {
          id: result.system_id,
          name: (result.system as string) || result.system_id,
          empire: '',
          police_level: 0,
        } as GameState['System'];
      }
      hasData = true;
    }
  }
  // get_skills response includes player_skills with next_level_xp per skill.
  if (Array.isArray(p.player_skills)) {
    console.log('[extractGameState] Found player_skills array:', p.player_skills);
    const nextXP: Record<string, number> = {};
    for (const entry of p.player_skills) {
      const e = entry as Record<string, unknown>;
      console.log('[extractGameState] Processing skill entry:', e);
      if (typeof e.skill_id === 'string' && typeof e.next_level_xp === 'number') {
        nextXP[e.skill_id] = e.next_level_xp;
        console.log(`[extractGameState] ${e.skill_id} -> ${e.next_level_xp}`);
      }
    }
    if (Object.keys(nextXP).length > 0) {
      state.SkillNextLevelXP = nextXP;
      hasData = true;
      console.log('[extractGameState] Set SkillNextLevelXP:', nextXP);
    }
  }

  return hasData ? state : null;
}

function mapToPlayer(gs: GameState): Player {
  const empire = (gs.Player?.empire || 'outerrim').toLowerCase();
  const policeLevel = gs.System?.police_level ?? 0;

  // The game server sends System with empty id field, so we need to fall back to name or CurrentSystem
  const systemId = gs.System?.id || gs.System?.name || gs.CurrentSystem || gs.Player?.current_system || '';

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
      systemId: systemId,
      system: gs.System?.name || gs.CurrentSystem || 'Unknown',
      poi: gs.CurrentPOI || gs.Player?.current_poi || 'Unknown',
      dockedAt: gs.Doc ? (gs.CurrentPOI || null) : null,
    },
    policeLevel: policeLevel > 0 ? 'policed' : 'lawless',
    tick: gs.CurrentTick ?? 0,
    traveling: gs.Traveling ?? false,
    travelProgress: gs.TravelProgress ?? 0,
    travelDestination: gs.TravelDestination ?? '',
    travelType: gs.TravelType,
    travelArrivalTick: gs.TravelArrivalTick ?? 0,
    travelStartTick: gs.TravelStartTick ?? 0,
  };
}

function mapToSkills(gs: GameState, skillDefinitions: Record<string, any>): Skill[] {
  const skills = gs.Player?.skills;
  if (!skills) return [];

  // If skill definitions haven't loaded yet, return skills with 0% XP
  if (!skillDefinitions || Object.keys(skillDefinitions).length === 0) {
    return Object.entries(skills).map(([name, skill]) => ({
      name,
      level: skill.level,
      xp: 0,
      nextLevelXp: 0,
    })).sort((a, b) => b.level - a.level || a.name.localeCompare(b.name));
  }

  return Object.entries(skills).map(([name, skill]) => {
    // Get next level XP from skill definitions
    const nextXP = getNextLevelXP(name, skill.level, skillDefinitions);

    let xpPct: number;
    if (nextXP > 0) {
      xpPct = (skill.xp / nextXP) * 100;
    } else {
      // Max level or unknown skill - show 100% or raw xp
      xpPct = skill.xp > 0 ? Math.min(skill.xp, 100) : 0;
    }

    return {
      name,
      level: skill.level,
      xp: Math.round(xpPct),
      nextLevelXp: nextXP,
    };
  }).sort((a, b) => b.level - a.level || a.name.localeCompare(b.name));
}

export type ConnectionStatus = 'disconnected' | 'connecting' | 'connected';

export interface CombatAlert {
  type: 'pirate_warning' | 'combat_start';
  pirateName: string;
  pirateTier: string;
  isBoss: boolean;
  message: string;
  delayTicks: number;
  pirateId: string;
  timestamp: number;
}

export interface ObserverState {
  status: ConnectionStatus;
  player: Player | null;
  skills: Skill[];
  agents: AgentInfo[];
  subscribedAgent: string | null;
  error: string | null;
  combatAlert: CombatAlert | null;
}

export function useObserver(wsUrl: string) {
  const wsRef = useRef<WebSocket | null>(null);
  const gameStateRef = useRef<Partial<GameState>>({});
  const { skills: skillDefinitions } = useSkillDefinitions();

  const [state, setState] = useState<ObserverState>({
    status: 'disconnected',
    player: null,
    skills: [],
    agents: [],
    subscribedAgent: null,
    error: null,
    combatAlert: null,
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
      combatAlert: null,
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
          // Detect pirate warning / combat events
          if (respType === 'pirate_warning') {
            setState(s => ({
              ...s,
              combatAlert: {
                type: 'pirate_warning',
                pirateName: (respPayload.pirate_name as string) || 'Unknown',
                pirateTier: (respPayload.pirate_tier as string) || 'unknown',
                isBoss: (respPayload.is_boss as boolean) || false,
                message: (respPayload.message as string) || 'Hostile detected!',
                delayTicks: (respPayload.delay_ticks as number) || 1,
                pirateId: (respPayload.pirate_id as string) || '',
                timestamp: Date.now(),
              },
            }));
          }

          const partial = extractGameState({ type: respType, payload: respPayload });
          if (partial) {
            gameStateRef.current = deepMerge(gameStateRef.current, partial);
            const gs = gameStateRef.current as GameState;
            setState(s => ({
              ...s,
              player: mapToPlayer(gs),
              skills: mapToSkills(gs, skillDefinitions),
            }));
          }
        } else {
          // Full state snapshot (e.g. from initial subscribe) — Go struct with uppercase keys.
          gameStateRef.current = deepMerge(gameStateRef.current, raw as Partial<GameState>);
          const gs = gameStateRef.current as GameState;
          setState(s => ({
            ...s,
            player: mapToPlayer(gs),
            skills: mapToSkills(gs, skillDefinitions),
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

  const clearError = useCallback(() => {
    setState(s => ({ ...s, error: null }));
  }, []);

  const dismissCombatAlert = useCallback(() => {
    setState(s => ({ ...s, combatAlert: null }));
  }, []);

  const sendCommand = useCallback((command: string, payload: Record<string, unknown>) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    if (!state.subscribedAgent) return;

    ws.send(JSON.stringify({
      type: 'command',
      agent: state.subscribedAgent,
      command,
      payload,
    }));
  }, [state.subscribedAgent]);

  return {
    ...state,
    connect,
    disconnect,
    subscribe,
    addAgent,
    removeAgent,
    listAgents,
    sendCommand,
    clearError,
    dismissCombatAlert,
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
