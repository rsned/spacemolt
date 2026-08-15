import { useMemo, useState } from 'react';
import { FLEETS } from '../../lib/useFleetStream';
import { useRoster, type RosterRow } from '../../lib/useRoster';

/** Fixed display order for the unlock pips; key -> single-glyph label. */
const UNLOCKS: Array<[string, string]> = [
  ['freight', 'F'],
  ['haul', 'H'],
  ['mission_delivery', 'D'],
  ['smuggling', 'S'],
  ['stronghold_access', 'St'],
];

type SortKey = 'agent' | 'fleet' | 'credits' | 'ship' | 'hull' | 'fuel' | 'cargo';

function Bar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <span className="inline-block w-14 h-1.5 bg-[#2a2618] rounded-sm align-middle mr-1.5">
      <span className="block h-full rounded-sm" style={{ width: `${pct}%`, background: color }} />
    </span>
  );
}

function barColor(cur: number, max: number): string {
  const pct = max > 0 ? cur / max : 0;
  if (pct < 0.25) return '#dc2626';
  if (pct < 0.6) return '#d4a017';
  return '#34d399';
}

function captureAge(iso: string): string {
  if (!iso) return 'never';
  const ms = Date.now() - new Date(iso).getTime();
  if (!Number.isFinite(ms)) return '?';
  const m = Math.floor(ms / 60000);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

function UnlockPips({ row }: { row: RosterRow }) {
  return (
    <span className="flex gap-1">
      {UNLOCKS.map(([key, label]) => {
        const c = row.capabilities[key];
        const title = c
          ? `${key}: ${c.eligible ? 'eligible' : `blocked${c.reason ? ` — ${c.reason}` : ''}`}`
          : `${key}: not evaluated`;
        const cls = !c
          ? 'border-[#2a2618] text-[#5a5545]'
          : c.eligible
            ? 'border-[#d4a017] text-[#d4a017] bg-[#d4a017]/10'
            : 'border-red-800 text-red-700';
        return (
          <span key={key} title={title}
            className={`inline-flex items-center justify-center w-5 h-4 text-[9px] uppercase tracking-wider border rounded-sm ${cls}`}>
            {label}
          </span>
        );
      })}
    </span>
  );
}

export function RosterTable({ onSelect }: { onSelect: (agentId: string) => void }) {
  const { data, error, loading } = useRoster();
  const [query, setQuery] = useState('');
  const [showStale, setShowStale] = useState(true);
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'agent', desc: false });

  const rows = useMemo(() => {
    if (!data) return [];
    const q = query.trim().toLowerCase();
    const filtered = data.filter((r) => {
      if (!showStale && r.stale) return false;
      if (!q) return true;
      return (r.agent_id + ' ' + r.username + ' ' + (r.fleet ?? '') + ' ' + (r.ship?.class_name ?? ''))
        .toLowerCase().includes(q);
    });
    const dir = sort.desc ? -1 : 1;
    // Percent-style columns return [fraction, capacity]: the fill level sorts
    // first, and among equal fills (a fleet of full tanks) the bigger tank
    // ranks higher instead of falling straight through to the name tiebreak.
    const val = (r: RosterRow): Array<string | number> => {
      switch (sort.key) {
        case 'fleet': return [r.fleet ?? '~']; // unfleeted sorts last
        case 'credits': return [r.credits];
        case 'ship': return [r.ship?.class_name ?? '~'];
        case 'hull': return r.ship && r.ship.hull_max > 0
          ? [r.ship.hull_current / r.ship.hull_max, r.ship.hull_max] : [-1, 0];
        case 'fuel': return r.ship && r.ship.fuel_max > 0
          ? [r.ship.fuel_current / r.ship.fuel_max, r.ship.fuel_max] : [-1, 0];
        case 'cargo': return [r.ship?.cargo_used ?? -1, r.ship?.cargo_capacity ?? 0];
        default: return [r.agent_id || r.username];
      }
    };
    return filtered.sort((a, b) => {
      const av = val(a); const bv = val(b);
      for (let i = 0; i < av.length; i++) {
        if (av[i] < bv[i]) return -dir;
        if (av[i] > bv[i]) return dir;
      }
      return a.agent_id < b.agent_id ? -1 : 1;
    });
  }, [data, query, showStale, sort]);

  const header = (key: SortKey, label: string, extra = '') => (
    <th
      className={`px-2 py-1.5 text-left text-[10px] uppercase tracking-widest text-[#8a8570] cursor-pointer select-none hover:text-[#d8d3c0] ${extra}`}
      onClick={() => setSort((s) => ({ key, desc: s.key === key ? !s.desc : false }))}
    >
      {label}{sort.key === key ? (sort.desc ? ' ▾' : ' ▴') : ''}
    </th>
  );

  if (loading) return <div className="p-6 text-xs text-[#8a8570]">Reading the ledger…</div>;
  if (error) return <div className="p-6 text-xs text-red-600">Roster failed to load: {error}</div>;
  if (!data?.length) {
    return <div className="p-6 text-xs text-[#8a8570]">No agents captured yet — the asset ledger fills in as fleets run.</div>;
  }

  const staleCount = data.filter((r) => r.stale).length;

  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center gap-3 px-3 py-1.5 border-b border-[#2a2618]">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter by agent, fleet, or hull"
          className="bg-[#11100c] border border-[#2a2618] rounded-sm px-2 py-1 text-xs w-64
            placeholder:text-[#5a5545] focus:outline-none focus:border-[#d4a017]"
        />
        <label className="flex items-center gap-1.5 text-[10px] uppercase tracking-widest text-[#8a8570] cursor-pointer">
          <input type="checkbox" checked={showStale} onChange={(e) => setShowStale(e.target.checked)} />
          stale ({staleCount})
        </label>
        <span className="ml-auto text-[10px] uppercase tracking-widest text-[#8a8570]">
          {rows.length} of {data.length} agents
        </span>
      </div>
      <div className="flex-1 overflow-auto">
        <table className="w-full text-xs border-collapse">
          <thead className="sticky top-0 bg-[#0a0a08] z-10">
            <tr className="border-b border-[#2a2618]">
              {header('agent', 'agent')}
              {header('fleet', 'fleet')}
              {header('credits', 'credits', 'text-right')}
              {header('ship', 'ship')}
              {header('hull', 'hull')}
              {header('fuel', 'fuel')}
              {header('cargo', 'cargo')}
              <th className="px-2 py-1.5 text-left text-[10px] uppercase tracking-widest text-[#8a8570]">unlocks</th>
              <th className="px-2 py-1.5 text-right text-[10px] uppercase tracking-widest text-[#8a8570]">captured</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const color = r.fleet ? FLEETS[r.fleet] ?? '#d8d3c0' : '#8a8570';
              return (
                <tr key={r.player_id}
                  onClick={() => onSelect(r.agent_id || r.player_id)}
                  className={`border-b border-[#1a1812] cursor-pointer hover:bg-[#11100c] ${r.stale ? 'opacity-40' : ''}`}>
                  <td className="px-2 py-1 font-bold whitespace-nowrap" style={{ color }}>
                    {r.agent_id || r.username}
                  </td>
                  <td className="px-2 py-1">
                    {r.fleet
                      ? <span className="px-1.5 text-[9px] uppercase tracking-widest border rounded-sm" style={{ color, borderColor: color }}>{r.fleet}</span>
                      : <span className="text-[#5a5545]">—</span>}
                  </td>
                  <td className="px-2 py-1 text-right tabular-nums">{Math.round(r.credits).toLocaleString()}</td>
                  <td className="px-2 py-1 whitespace-nowrap text-[#8a8570]">{r.ship?.class_name ?? '—'}</td>
                  <td className="px-2 py-1 whitespace-nowrap tabular-nums">
                    {r.ship && r.ship.hull_max > 0 && <>
                      <Bar value={r.ship.hull_current} max={r.ship.hull_max} color={barColor(r.ship.hull_current, r.ship.hull_max)} />
                      {r.ship.hull_current}/{r.ship.hull_max}
                    </>}
                  </td>
                  <td className="px-2 py-1 whitespace-nowrap tabular-nums">
                    {r.ship && r.ship.fuel_max > 0 && <>
                      <Bar value={r.ship.fuel_current} max={r.ship.fuel_max} color={barColor(r.ship.fuel_current, r.ship.fuel_max)} />
                      {r.ship.fuel_current}/{r.ship.fuel_max}
                    </>}
                  </td>
                  <td className="px-2 py-1 whitespace-nowrap tabular-nums">
                    {r.ship ? `${r.ship.cargo_used}${r.ship.cargo_capacity ? `/${r.ship.cargo_capacity}` : ''}` : ''}
                  </td>
                  <td className="px-2 py-1"><UnlockPips row={r} /></td>
                  <td className="px-2 py-1 text-right text-[#8a8570] tabular-nums">{captureAge(r.captured_at)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
