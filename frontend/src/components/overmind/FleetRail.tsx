import { useMemo, useState } from 'react';
import { FLEETS, type AgentState } from '../../lib/useFleetStream';
import { AgentCard } from './AgentCard';

export function FleetRail({ agents, offMap, staleFleets, selectedId, onSelect, highlightedIds }: {
  agents: AgentState[]; offMap: AgentState[]; staleFleets: string[];
  selectedId: string | null; onSelect: (id: string) => void;
  highlightedIds?: ReadonlySet<string>;
}) {
  const [filter, setFilter] = useState('');
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const stale = useMemo(() => new Set(staleFleets), [staleFleets]);

  const groups = useMemo(() => {
    const g = new Map<string, AgentState[]>();
    Object.keys(FLEETS).forEach((f) => g.set(f, []));
    [...agents, ...offMap]
      .filter((a) => !filter || a.agent_id.includes(filter) || a.system_name.toLowerCase().includes(filter.toLowerCase()))
      .forEach((a) => g.get(a.fleet)?.push(a) ?? g.set(a.fleet, [a]));
    // Unhealthy first, then by id, within each fleet.
    for (const list of g.values()) {
      list.sort((x, y) =>
        Number(y.healthy && y.seen) - Number(x.healthy && x.seen) === 0
          ? x.agent_id.localeCompare(y.agent_id)
          : Number(x.healthy && x.seen) - Number(y.healthy && y.seen));
    }
    return g;
  }, [agents, offMap, filter]);

  return (
    <div className="p-2">
      <input
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="filter agents…"
        className="w-full mb-2 px-2 py-1 bg-[#0a0a08] border border-[#2a2618] rounded-sm text-xs text-[#d8d3c0]"
      />
      {[...groups.entries()].map(([fleet, list]) => {
        const color = FLEETS[fleet] ?? '#d8d3c0';
        const credits = list.reduce((s, a) => s + a.credits, 0);
        const isCollapsed = collapsed[fleet];
        const worstUnhealthy = list.some((a) => !a.healthy || !a.seen);
        return (
          <div key={fleet} className="mb-2">
            <button
              onClick={() => setCollapsed((c) => ({ ...c, [fleet]: !c[fleet] }))}
              className="w-full flex items-center justify-between text-xs uppercase tracking-widest py-1"
              style={{ color }}
            >
              <span>
                {isCollapsed ? '▸' : '▾'} {fleet} <span className="text-[#8a8570]">{list.length}</span>
                <span className={`ml-1 ${worstUnhealthy ? 'text-red-500' : 'text-emerald-500'}`}>◉</span>
              </span>
              <span className="font-mono text-[#8a8570]">₡ {Math.round(credits).toLocaleString()}</span>
            </button>
            {!isCollapsed && list.map((a) => (
              <AgentCard key={a.agent_id} agent={a} color={color}
                selected={selectedId === a.agent_id} stale={stale.has(fleet)}
                highlighted={highlightedIds?.has(a.agent_id) ?? false}
                onClick={() => onSelect(a.agent_id)} />
            ))}
          </div>
        );
      })}
    </div>
  );
}
