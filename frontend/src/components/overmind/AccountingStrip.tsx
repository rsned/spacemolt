import type { Accounting } from '../../lib/useFleetStream';
import { shortVersion } from '../../lib/version';

function cr(n: number): string {
  return Math.round(n).toLocaleString();
}

function Stat({ label, value, warn, title }: { label: string; value: string; warn?: boolean; title?: string }) {
  return (
    <div className="px-4 border-r border-[#2a2618] last:border-r-0" title={title}>
      <div className="text-[13px] uppercase tracking-widest text-[#8a8570]">{label}</div>
      <div className={`font-mono text-xl ${warn ? 'text-red-400' : 'text-[#d4a017]'}`}>{value}</div>
    </div>
  );
}

export function AccountingStrip({ accounting, agentCount, staleFleets, connected,
  currentOvermind, currentWorker }: {
  accounting: Accounting | null;
  agentCount: number;
  staleFleets: string[];
  connected: boolean;
  /** Newest build seen for each binary — the reference every fleet badge is
   * coloured against. Shown here so "what is current?" is answerable without
   * expanding a fleet and eyeballing which green pill wins. */
  currentOvermind: string;
  currentWorker: string;
}) {
  const a = accounting;
  return (
    <div className="flex items-center bg-[#11100c] border-b border-[#2a2618] py-2">
      <div className="px-4 text-[#d4a017] font-bold tracking-widest text-base">FLEET ACCOUNTING</div>
      <Stat label="credits" value={a ? `₡ ${cr(a.total_credits)}` : '—'} />
      <Stat label="agents" value={a ? `${a.healthy}/${a.agents} healthy` : `${agentCount}`}
        warn={!!a && a.healthy < a.agents} />
      {/* All four rates share the same trailing-24h window; the first is
          simply the sum of the three streams, so label it as the total. */}
      <Stat label="total earn/hr" value={a ? cr(a.combined_per_hour) : '—'}
        title="haul + freight + missions, 24h trailing average" />
      <Stat label="haul/hr" value={a ? cr(a.haul.per_hour) : '—'} title="24h trailing average" />
      <Stat label="freight/hr" value={a ? cr(a.freight.per_hour) : '—'} title="24h trailing average" />
      <Stat label="missions/hr" value={a ? cr(a.missions.per_hour) : '—'} title="24h trailing average" />
      <Stat label="restarts" value={a ? `${a.restarts}` : '—'} warn={!!a && a.restarts > 0} />
      <div className="flex-1" />
      {staleFleets.length > 0 && (
        <div className="px-3 text-sm text-amber-500">stale: {staleFleets.join(' ')}</div>
      )}
      {(currentOvermind || currentWorker) && (
        <div className="px-3 text-[13px] font-mono leading-tight text-[#8a8570] text-right"
          title={`newest build seen — overmind ${currentOvermind || '—'}, worker ${currentWorker || '—'}`}>
          <div>ov {shortVersion(currentOvermind) || '—'}</div>
          <div>w {shortVersion(currentWorker) || '—'}</div>
        </div>
      )}
      <div className={`px-4 text-sm ${connected ? 'text-emerald-500' : 'text-red-500'}`}>
        {connected ? '● live' : '○ reconnecting'}
      </div>
    </div>
  );
}
