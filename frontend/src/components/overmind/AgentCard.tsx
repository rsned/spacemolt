import type { AgentState } from '../../lib/useFleetStream';

function Bar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <span className="inline-block w-16 h-1.5 bg-[#2a2618] rounded-sm align-middle mx-1">
      <span className="block h-full rounded-sm" style={{ width: `${pct}%`, background: color }} />
    </span>
  );
}

export function AgentCard({ agent, color, selected, stale, onClick }: {
  agent: AgentState; color: string; selected: boolean; stale: boolean; onClick: () => void;
}) {
  const unhealthy = !agent.healthy || !agent.seen;
  return (
    <button
      onClick={onClick}
      className={`w-full text-left mb-2 p-2 border rounded-sm bg-[#11100c] text-xs
        ${selected ? 'border-[#d4a017]' : unhealthy ? 'border-red-700' : 'border-[#2a2618]'}
        ${stale ? 'opacity-50' : ''}`}
    >
      <div className="flex items-center justify-between border-b border-[#2a2618] pb-1 mb-1">
        <span className="font-bold" style={{ color }}>{agent.agent_id}</span>
        <span className={unhealthy ? 'text-red-500' : 'text-emerald-500'}>◉</span>
      </div>
      <div className="text-[#8a8570] truncate">
        {agent.system_name} / {agent.poi}{agent.docked ? ' ⚓' : ''}
      </div>
      <div className="font-mono text-[#d8d3c0]">
        ₡ {Math.round(agent.credits).toLocaleString()}
        <span className="text-[#8a8570]"> hull</span>
        <Bar value={agent.hull} max={agent.max_hull} color="#34d399" />
        <span className="text-[#8a8570]">fuel</span>
        <Bar value={agent.fuel} max={agent.max_fuel} color="#22d3ee" />
      </div>
      <div className="font-mono text-[#8a8570]">
        cargo <Bar value={agent.cargo_used} max={agent.cargo_capacity} color="#d4a017" />
        {Math.round(agent.cargo_used)}/{Math.round(agent.cargo_capacity)}
      </div>
      {agent.activity && <div className="text-[#d4a017] truncate">► {agent.activity}</div>}
      <div className="text-[#8a8570]">restarts {agent.restarts}</div>
    </button>
  );
}
