import { memo, useCallback, useRef, useState } from 'react';
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
function Row({ label, value, bar, title }: { label: string; value: string; bar?: React.ReactNode; title?: string }) {
  return (
    <div className="flex items-center justify-between gap-2 leading-snug" title={title}>
      <span className="text-[#8a8570] shrink-0">{label}</span>
      <span className="flex items-center gap-1.5 min-w-0">
        {bar}
        <span className="font-mono tabular-nums text-[#d8d3c0] whitespace-nowrap">{value}</span>
      </span>
    </div>
  );
}

/** The figures behind an opportunity, as served by /api/overmind/opportunity.
    Jumps is -1 when no route is known: zero would read as "same station, free
    run", which is the most expensive possible misreading. */
type OppDetail = {
  id: number; item_id: string; status?: string;
  buy_price: number; sell_price: number; unit_margin: number; margin_pct: number;
  quantity: number; source_units: number; gross_profit: number;
  from_name?: string; from_system?: string; to_name?: string; to_system?: string;
  jumps: number; profit_per_jump: number;
  claimed_by?: string; expires_at?: string; notes?: string;
};

const num = (n: number) => Math.round(n).toLocaleString();

/** One label/value line inside the drawer, matching the card's Row rhythm. */
function Spec({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2" title={hint}>
      <span className="text-[#8a8570] shrink-0">{label}</span>
      <span className="font-mono tabular-nums text-[#d8d3c0] text-right">{value}</span>
    </div>
  );
}

/** The expanded drawer. Shows what the activity clause cannot: the margin, the
    route length, and -- the distinction that actually decides a haul -- how
    much the DESTINATION will absorb versus how deep the seller's book is. A
    book 39,608 deep whose buyer wants 30 is a 30-unit trade. */
function OppDrawer({ detail }: { detail: OppDetail }) {
  const route = detail.jumps < 0 ? 'unknown' : `${detail.jumps} jump${detail.jumps === 1 ? '' : 's'}`;
  const perJump = detail.jumps > 0 ? ` \u00b7 ${num(detail.profit_per_jump)} cr/jump` : '';
  return (
    <div className="mt-1 ml-3 pl-2 border-l border-[#2a2618] space-y-0.5">
      <Spec label="Buy" value={`${num(detail.buy_price)} cr/u`}
        hint={detail.from_name ? `${detail.from_name}${detail.from_system ? ` (${detail.from_system})` : ''}` : undefined} />
      <Spec label="Sell" value={`${num(detail.sell_price)} cr/u`}
        hint={detail.to_name ? `${detail.to_name}${detail.to_system ? ` (${detail.to_system})` : ''}` : undefined} />
      <Spec label="Margin" value={`${num(detail.unit_margin)} cr/u \u00b7 ${Math.round(detail.margin_pct)}%`} />
      <Spec label="Route" value={`${route}${perJump}`} />
      <Spec label="Qty" value={`${num(detail.quantity)} of ${num(detail.source_units)}`}
        hint="destination absorbs / seller's book depth" />
      <Spec label="Profit" value={`${num(detail.gross_profit)} cr`} />
      {detail.claimed_by && <Spec label="Claimed" value={detail.claimed_by} />}
      {detail.notes && <Spec label="Notes" value={detail.notes} />}
    </div>
  );
}

/** The activity string is the only place an opportunity's terms appear, and it
    is built as " · "-joined clauses. Truncating it hid the route entirely, and
    letting it wrap broke mid-clause. Splitting on the separator puts one clause
    per line, continuation lines keeping their "·" so the grouping stays legible.

    When the text names an opportunity, the leading marker becomes a disclosure:
    the clauses alone say what is being hauled but never whether it is worth
    hauling, and a 1900-cargo hull committed to one unit over 21 jumps looks
    identical to a full load until the numbers are expanded. Detail is fetched
    on first open only, so collapsed cards cost nothing. */
function Activity({ text }: { text: string }) {
  const parts = text.split(' · ');
  const oppID = text.match(/Opportunity #(\d+)/)?.[1];
  const [open, setOpen] = useState(false);
  const [detail, setDetail] = useState<OppDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Guards against a second fetch while the first is still in flight, and
  // against refetching when the drawer is reopened.
  const loading = useRef(false);

  const toggle = useCallback((e: React.MouseEvent | React.KeyboardEvent) => {
    // The whole card is a button that selects the agent; without this, opening
    // the drawer would also change the selection.
    e.stopPropagation();
    e.preventDefault();
    const next = !open;
    setOpen(next);
    if (next && oppID && !detail && !loading.current) {
      loading.current = true;
      fetch(`/api/overmind/opportunity/${oppID}`)
        .then((r) => (r.ok ? r.json() : Promise.reject(new Error(r.status === 404 ? 'expired or pruned' : `HTTP ${r.status}`))))
        .then((d: OppDetail) => setDetail(d))
        .catch((err: Error) => setError(err.message))
        .finally(() => { loading.current = false; });
    }
  }, [open, oppID, detail]);

  return (
    <div className="text-[#d4a017] leading-snug">
      {parts.map((part, i) => (
        <div key={i} className={i === 0 ? '' : 'pl-3'} title={i === 0 ? text : undefined}>
          {i === 0 ? (
            oppID ? (
              <span
                role="button"
                tabIndex={0}
                aria-expanded={open}
                aria-label={open ? 'Hide opportunity details' : 'Show opportunity details'}
                onClick={toggle}
                onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggle(e); }}
                className="cursor-pointer select-none hover:text-[#f0c040] focus:outline-none focus:text-[#f0c040]"
              >
                <span className="inline-block w-3">{open ? '\u25BC' : '\u25BA'}</span>{part}
              </span>
            ) : (
              <span className="cursor-help"><span className="inline-block w-3">{'\u25BA'}</span>{part}</span>
            )
          ) : (
            <><span className="inline-block w-3">{'\u00B7'}</span>{part}</>
          )}
        </div>
      ))}
      {open && detail && <OppDrawer detail={detail} />}
      {open && !detail && !error && <div className="ml-3 pl-2 text-[#8a8570]">{'loading\u2026'}</div>}
      {open && error && <div className="ml-3 pl-2 text-red-500">{error}</div>}
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
      {!!agent.cargo_value && (
        /* Credits shown above exclude everything the agent has already spent
           on this run. Without this line a working hauler reads as poorer
           than an idle one, and the fleet total looks like it is bleeding
           whenever several are mid-cycle. */
        <Row
          label="In cargo"
          value={`₡ ${Math.round(agent.cargo_value).toLocaleString()}`}
          title={`${agent.cargo_item ?? 'goods'}${agent.cargo_proceeds ? ` · expects ₡ ${Math.round(agent.cargo_proceeds).toLocaleString()} back` : ''}`}
        />
      )}
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
