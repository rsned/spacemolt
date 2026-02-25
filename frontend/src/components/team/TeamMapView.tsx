import { useState, useEffect } from 'react';

interface TeamMember {
  agent_id: string;
  role: string;
  strategy?: string;
}

interface StatusReport {
  agent_id: string;
  system: string;
  poi: string;
  current_action: string;
  docked: boolean;
  in_combat: boolean;
  hull: number;
  fuel: number;
  credits: number;
  cargo_used: number;
  cargo_capacity: number;
}

interface Team {
  id: string;
  name: string;
  leader_id: string;
  members: TeamMember[];
}

interface TeamMapViewProps {
  teamId: string;
}

const ROLE_COLORS: Record<string, string> = {
  captain: '#f0c040',
  interceptor: '#ff6060',
  gunner: '#ff4040',
  tactical: '#60a0ff',
  salvage: '#60d060',
  default: '#a0a0c0',
};

export function TeamMapView({ teamId }: TeamMapViewProps) {
  const [team, setTeam] = useState<Team | null>(null);
  const [members, setMembers] = useState<StatusReport[]>([]);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);

  useEffect(() => {
    const fetchTeam = async () => {
      try {
        const res = await fetch(`/api/teams/${teamId}`);
        if (res.ok) {
          const data = await res.json();
          setTeam(data.team);
          setMembers(data.members || []);
        }
      } catch (err) {
        console.error('Failed to fetch team:', err);
      }
    };

    fetchTeam();
    const interval = setInterval(fetchTeam, 5000);
    return () => clearInterval(interval);
  }, [teamId]);

  if (!team) {
    return <div className="p-4 text-gray-500">Loading team...</div>;
  }

  const getMemberRole = (agentId: string): string => {
    return team.members.find(m => m.agent_id === agentId)?.role || 'unknown';
  };

  const getRoleColor = (role: string): string => {
    return ROLE_COLORS[role] || ROLE_COLORS.default;
  };

  // Group members by system for the map display
  const systemGroups = new Map<string, StatusReport[]>();
  for (const m of members) {
    const sys = m.system || 'unknown';
    if (!systemGroups.has(sys)) systemGroups.set(sys, []);
    systemGroups.get(sys)!.push(m);
  }

  return (
    <div className="flex flex-col h-full bg-gray-950">
      {/* Top bar */}
      <div className="flex items-center gap-4 px-4 py-2 bg-gray-900 border-b border-gray-800">
        <h2 className="text-lg font-bold text-gray-200">{team.name}</h2>
        <span className="text-xs text-gray-500">
          Leader: {team.leader_id} | {members.length} members
        </span>
      </div>

      <div className="flex flex-1 overflow-hidden">
        {/* Left sidebar - Team Roster */}
        <div className="w-64 border-r border-gray-800 overflow-y-auto">
          <div className="p-2">
            <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wider mb-2">
              Roster
            </h3>
            {team.members.map(member => {
              const status = members.find(m => m.agent_id === member.agent_id);
              const color = getRoleColor(member.role);
              const isSelected = selectedAgent === member.agent_id;

              return (
                <button
                  key={member.agent_id}
                  onClick={() => setSelectedAgent(isSelected ? null : member.agent_id)}
                  className={`w-full text-left p-2 rounded mb-1 transition-colors ${
                    isSelected ? 'bg-gray-800' : 'hover:bg-gray-900'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span
                      className="w-2 h-2 rounded-full"
                      style={{ backgroundColor: color }}
                    />
                    <span className="text-sm text-gray-300">{member.agent_id}</span>
                  </div>
                  <div className="text-xs text-gray-600 ml-4">
                    {member.role}
                    {member.strategy && ` / ${member.strategy}`}
                  </div>
                  {status && (
                    <div className="ml-4 mt-1 text-xs">
                      <span className="text-gray-500">{status.system}</span>
                      {status.docked && <span className="text-green-600 ml-1">[D]</span>}
                      {status.in_combat && <span className="text-red-500 ml-1">[!</span>}
                      <div className="flex gap-2 mt-0.5">
                        <span className="text-gray-600">
                          H:{Math.round(status.hull)}
                        </span>
                        <span className="text-gray-600">
                          F:{Math.round(status.fuel)}
                        </span>
                        <span className="text-gray-600">
                          C:{Math.round(status.cargo_used)}/{Math.round(status.cargo_capacity)}
                        </span>
                      </div>
                    </div>
                  )}
                </button>
              );
            })}
          </div>
        </div>

        {/* Central area - System Map */}
        <div className="flex-1 p-4 overflow-auto">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {Array.from(systemGroups.entries()).map(([system, agents]) => (
              <div key={system} className="bg-gray-900 rounded-lg p-3 border border-gray-800">
                <h4 className="text-sm font-semibold text-gray-400 mb-2">{system}</h4>
                <div className="space-y-1">
                  {agents.map(agent => {
                    const role = getMemberRole(agent.agent_id);
                    const color = getRoleColor(role);
                    const isHighlighted = selectedAgent === agent.agent_id;

                    return (
                      <div
                        key={agent.agent_id}
                        className={`flex items-center gap-2 px-2 py-1 rounded text-xs ${
                          isHighlighted ? 'bg-gray-800 ring-1 ring-blue-600' : ''
                        }`}
                      >
                        <span
                          className="w-3 h-3 rounded-sm flex-shrink-0"
                          style={{ backgroundColor: color }}
                          title={role}
                        />
                        <span className="text-gray-300">{agent.agent_id}</span>
                        <span className="text-gray-600 truncate">{agent.poi || ''}</span>
                        {agent.docked && <span className="text-green-600">[D]</span>}
                        {agent.in_combat && <span className="text-red-500 animate-pulse">[!]</span>}
                      </div>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>

          {members.length === 0 && (
            <div className="text-center text-gray-600 mt-8">
              No member status data available
            </div>
          )}
        </div>
      </div>

      {/* Bottom panel - Action Log */}
      <TeamActionLog teamId={teamId} memberColors={
        Object.fromEntries(
          team.members.map(m => [m.agent_id, getRoleColor(m.role)])
        )
      } />
    </div>
  );
}

interface ActionLogEntry {
  timestamp: string;
  team_id: string;
  agent_id: string;
  action: string;
  status: string;
}

interface TeamActionLogProps {
  teamId: string;
  memberColors: Record<string, string>;
}

function TeamActionLog({ teamId, memberColors }: TeamActionLogProps) {
  const [entries, setEntries] = useState<ActionLogEntry[]>([]);

  useEffect(() => {
    const fetchLog = async () => {
      try {
        const res = await fetch(`/api/teams/${teamId}/log?limit=50`);
        if (res.ok) {
          const data = await res.json();
          setEntries(data);
        }
      } catch (err) {
        console.error('Failed to fetch team log:', err);
      }
    };

    fetchLog();
    const interval = setInterval(fetchLog, 5000);
    return () => clearInterval(interval);
  }, [teamId]);

  return (
    <div className="h-32 border-t border-gray-800 overflow-y-auto bg-gray-900">
      <div className="px-4 py-1 text-xs font-semibold text-gray-500 uppercase tracking-wider sticky top-0 bg-gray-900">
        Action Log
      </div>
      <div className="px-4 pb-2 space-y-0.5">
        {entries.length === 0 ? (
          <div className="text-xs text-gray-600">No actions recorded</div>
        ) : (
          entries.map((entry, i) => (
            <div key={i} className="text-xs flex gap-2">
              <span className="text-gray-600 flex-shrink-0">
                {new Date(entry.timestamp).toLocaleTimeString()}
              </span>
              <span
                className="flex-shrink-0 font-medium"
                style={{ color: memberColors[entry.agent_id] || '#a0a0c0' }}
              >
                {entry.agent_id}
              </span>
              <span className="text-gray-400">{entry.action}</span>
              <span className="text-gray-600 truncate">{entry.status}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
