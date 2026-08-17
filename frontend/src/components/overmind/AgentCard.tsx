import { memo } from 'react';
import type { AgentState } from '../../lib/useFleetStream';

function Bar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <span className="inline-block w-16 h-1.5 bg-[#2a2618] rounded-sm align-middle mx-1">
      <span className="block h-full rounded-sm" style={{ width: `${pct}%`, background: color }} />
    </span>
  );
}

/** Terse relative age (e.g. "3s", "2m", "1h") for the seen-age footer. */
function seenAge(lastSeen: string): string {
  const ms = Date.now() - new Date(lastSeen).getTime();
  if (!Number.isFinite(ms) || ms < 0) return '0s';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return `${h}h`;
}

export const AgentCard = memo(function AgentCard({ agent, color, selected, stale, highlighted = false, onClick, onRemove }: {
  agent: AgentState; color: string; selected: boolean; stale: boolean;
  highlighted?: boolean; onClick: () => void; onRemove?: (agent: AgentState) => void;
}) {
  const unhealthy = !agent.healthy || !agent.seen;
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onClick(); } }}
      className={`group w-full text-left mb-2 p-2 border rounded-sm bg-[#11100c] text-xs cursor-pointer
        ${selected ? 'border-[#d4a017]' : unhealthy ? 'border-red-700' : 'border-[#2a2618]'}
        ${stale ? 'opacity-50' : ''}
        ${highlighted ? 'ring-1 ring-[#22d3ee]' : ''}`}
    >
      <div className="flex items-center justify-between border-b border-[#2a2618] pb-1 mb-1">
        <span className="font-bold" style={{ color }}>{agent.agent_id}</span>
        <span className="flex items-center gap-1">
          {agent.leaving && (
            <span className="px-1 text-[10px] uppercase tracking-widest rounded-sm border border-[#d4a017] text-[#d4a017] bg-[#d4a017]/10">
              draining
            </span>
          )}
          {onRemove && (
            <button
              onClick={(e) => { e.stopPropagation(); onRemove(agent); }}
              title="Remove from fleet"
              className="opacity-0 group-hover:opacity-100 text-[#8a8570] hover:text-red-500 px-1"
            >
              ✕
            </button>
          )}
          <span className={unhealthy ? 'text-red-500' : 'text-emerald-500'}>◉</span>
        </span>
      </div>
      <div className="text-[#8a8570] truncate" title={`${agent.system_name} / ${agent.poi}`}>
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
      {/* title= gives the full text on hover: the activity line is the only
          place an opportunity's terms (item, quantity, route) are shown, and
          it truncates well before the end of them. */}
      {agent.activity && (
        <div className="text-[#d4a017] truncate cursor-help" title={agent.activity}>
          ► {agent.activity}
        </div>
      )}
      <div className="text-[#8a8570]">restarts {agent.restarts} · seen {seenAge(agent.last_seen)}</div>
    </div>
  );
});
