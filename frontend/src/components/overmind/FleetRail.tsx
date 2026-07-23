import { useMemo, useState } from 'react';
import { FLEETS, TIER_COLORS, type AgentState, type OvermindInfo, type Tier } from '../../lib/useFleetStream';
import { AgentCard } from './AgentCard';

export function FleetRail({
  agents, offMap, staleFleets, selectedId, onSelect, highlightedIds,
  removed, onRemove, onReadd, overminds,
}: {
  agents: AgentState[]; offMap: AgentState[]; staleFleets: string[];
  selectedId: string | null; onSelect: (id: string) => void;
  highlightedIds?: ReadonlySet<string>;
  removed?: Record<string, string[]>;
  onRemove?: (agent: AgentState) => void;
  onReadd?: (fleet: string, agentId: string) => void;
  overminds?: Record<string, OvermindInfo>;
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

  // Group a fleet's workers by version string, worst-tier and any raw-modified
  // marker per group, for the "v0.3.0 ×35  v0.2.9 ×6" summary line.
  const versionGroups = (list: AgentState[]): { version: string; count: number; tier: Tier; modified: boolean }[] => {
    const m = new Map<string, { count: number; tier: Tier; modified: boolean }>();
    const rank: Record<Tier, number> = { green: 0, yellow: 1, red: 2 };
    for (const a of list) {
      const version = a.version || 'legacy';
      const tier = a.tier ?? 'red';
      const cur = m.get(version);
      if (cur) {
        cur.count += 1;
        if (rank[tier] > rank[cur.tier]) cur.tier = tier;
        cur.modified = cur.modified || !!a.modified;
      } else {
        m.set(version, { count: 1, tier, modified: !!a.modified });
      }
    }
    return [...m.entries()]
      .map(([version, v]) => ({ version, ...v }))
      .sort((x, y) => y.count - x.count);
  };

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
        const ov = overminds?.[fleet];
        const fleetTier: Tier = ov?.fleet_tier ?? 'red';
        const vGroups = versionGroups(list);
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
              <span className="flex items-center gap-1">
                {ov?.version && (
                  <span
                    className="px-1 text-[9px] font-mono rounded-sm border"
                    style={{ color: TIER_COLORS[fleetTier], borderColor: TIER_COLORS[fleetTier] }}
                    title={`overmind ${ov.version}${ov.commit ? ` (${ov.commit})` : ''}${ov.modified ? ' *' : ''}`}
                  >
                    {ov.version}{ov.modified ? ' *' : ''}
                  </span>
                )}
                <span className="font-mono text-[#8a8570]">₡ {Math.round(credits).toLocaleString()}</span>
              </span>
            </button>
            {!isCollapsed && vGroups.length > 0 && (
              <div className="flex flex-wrap gap-x-2 gap-y-0.5 px-1 pb-1 text-[9px] font-mono">
                {vGroups.map((g) => (
                  <span key={g.version} style={{ color: TIER_COLORS[g.tier] }}>
                    {g.version}{g.modified ? '*' : ''} ×{g.count}
                  </span>
                ))}
              </div>
            )}
            {!isCollapsed && list.map((a) => (
              <AgentCard key={a.agent_id} agent={a} color={color}
                selected={selectedId === a.agent_id} stale={stale.has(fleet)}
                highlighted={highlightedIds?.has(a.agent_id) ?? false}
                onClick={() => onSelect(a.agent_id)} onRemove={onRemove} />
            ))}
            {!isCollapsed && (removed?.[fleet]?.length ?? 0) > 0 && (
              <div className="mt-1 pt-1 border-t border-[#2a2618]">
                <div className="text-[10px] uppercase tracking-widest text-[#8a8570] mb-1">removed</div>
                {removed![fleet].map((id) => (
                  <div key={id} className="flex items-center justify-between text-xs text-[#8a8570] mb-1">
                    <span className="truncate">{id}</span>
                    <button
                      onClick={() => onReadd?.(fleet, id)}
                      className="px-1.5 py-0.5 text-[10px] uppercase tracking-widest border border-[#2a2618] rounded-sm text-[#d8d3c0] hover:border-[#d4a017] hover:text-[#d4a017]"
                    >
                      Re-add
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
