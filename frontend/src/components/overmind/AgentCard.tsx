import { memo } from 'react';
import type { AgentState } from '../../lib/useFleetStream';

function Bar({ value, max, color }: { value: number; max: number; color: string }) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <span className="inline-block w-16 h-1.5 bg-[#2a2618] rounded-sm align-middle shrink-0">
      <span className="block h-full rounded-sm" style={{ width: `${pct}%`, background: color }} />
    </span>
  );
}

/** One label/value line. Label hugs the left, value the right, so the numerals
    form a clean right-aligned column down the card instead of floating wherever
    the preceding text happened to end. tabular-nums keeps the digits from
    shifting the column as values change. */
function Row({ label, value, bar }: { label: string; value: string; bar?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2 leading-snug">
      <span className="text-[#8a8570] shrink-0">{label}</span>
      <span className="flex items-center gap-1.5 min-w-0">
        {bar}
        <span className="font-mono tabular-nums text-[#d8d3c0] whitespace-nowrap">{value}</span>
      </span>
    </div>
  );
}

/** The activity string is the only place an opportunity's terms appear, and it
    is built as " · "-joined clauses. Truncating it hid the route entirely, and
    letting it wrap broke mid-clause. Splitting on the separator puts one clause
    per line, continuation lines keeping their "·" so the grouping stays legible. */
function Activity({ text }: { text: string }) {
  const parts = text.split(' · ');
  return (
    <div className="text-[#d4a017] cursor-help leading-snug" title={text}>
      {parts.map((part, i) => (
        <div key={i} className={i === 0 ? '' : 'pl-3'}>
          {i === 0 ? '\u25BA ' : '\u00B7 '}{part}
        </div>
      ))}
    </div>
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

/** Catalog class ids arrive snake_cased and lowercase ("junk_convoy"); the card
    shows them the way the shipyard does. */
function prettyClass(id: string): string {
  return id.split('_').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
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
      className={`group w-full text-left mb-2 p-2 border rounded-sm bg-[#11100c] text-sm cursor-pointer
        ${selected ? 'border-[#d4a017]' : unhealthy ? 'border-red-700' : 'border-[#2a2618]'}
        ${stale ? 'opacity-50' : ''}
        ${highlighted ? 'ring-1 ring-[#22d3ee]' : ''}`}
    >
      <div className="flex items-center justify-between border-b border-[#2a2618] pb-1 mb-1">
        <span className="font-bold" style={{ color }}>{agent.agent_id}</span>
        <span className="flex items-center gap-1">
          {agent.leaving && (
            <span className="px-1 text-[13px] uppercase tracking-widest rounded-sm border border-[#d4a017] text-[#d4a017] bg-[#d4a017]/10">
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
      <div className="text-[#8a8570] truncate mb-1" title={`${agent.system_name} / ${agent.poi}`}>
        {agent.system_name} / {agent.poi}{agent.docked ? ' ⚓' : ''}
      </div>
      {agent.ship_class && <Row label="Ship" value={prettyClass(agent.ship_class)} />}
      <Row label="Credits" value={`₡ ${Math.round(agent.credits).toLocaleString()}`} />
      <Row label="Hull" value={`${Math.round(agent.hull)}/${Math.round(agent.max_hull)}`}
        bar={<Bar value={agent.hull} max={agent.max_hull} color="#34d399" />} />
      <Row label="Fuel" value={`${Math.round(agent.fuel)}/${Math.round(agent.max_fuel)}`}
        bar={<Bar value={agent.fuel} max={agent.max_fuel} color="#22d3ee" />} />
      <Row label="Cargo" value={`${Math.round(agent.cargo_used)}/${Math.round(agent.cargo_capacity)}`}
        bar={<Bar value={agent.cargo_used} max={agent.cargo_capacity} color="#d4a017" />} />
      {agent.quiesced && (
        /* A parked worker is idle on purpose. Without this it reads as a
           healthy agent doing nothing, which is exactly what a wedged one
           looks like. Amber, not red: this is not a fault. */
        <div className="mt-1 text-[#d4a017]" title={agent.quiesce_reason || 'parked by operator'}>
          ⏸ PARKED{agent.quiesce_reason ? ` · ${agent.quiesce_reason}` : ''}
        </div>
      )}
      {agent.activity && <div className="mt-1"><Activity text={agent.activity} /></div>}
      <div className="text-[#8a8570] mt-1">restarts {agent.restarts} · seen {seenAge(agent.last_seen)}</div>
    </div>
  );
});
