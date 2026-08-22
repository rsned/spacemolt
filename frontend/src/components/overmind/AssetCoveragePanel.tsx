import type { AssetCoverageRow } from '../../lib/useFleetStream';

// Share of an source's agents that must be stale before the row reads as a
// stall rather than a straggler.
//
// Severity comes from `stale` alone, which pkg/assets/coverage.go computes
// per-agent against 2x each source's cadence. The panel deliberately does NOT
// re-derive staleness from `oldest`: that column is a MIN across every agent,
// so a single agent nobody captures pins it forever and paints the row red
// while the other 85 refresh on time -- exactly what craftsman-1 (no worker
// runs it, so it only captures when someone runs play_as by hand) did on
// 2026-08-08, showing "14.7h" on four rows that were 85/86 fresh.
//
// Dropping that clause also removes the duty to keep a cadence map in sync
// with the Go one.
const STALL_SHARE = 0.1;

// faction_storage is keyed on faction_id, so its count is factions, not agents.
const COUNT_LABEL: Record<string, string> = {
  faction_storage: 'factions',
};

function ageHours(oldest: string): number | null {
  if (!oldest) return null;
  const t = Date.parse(oldest);
  if (Number.isNaN(t)) return null;

  return (Date.now() - t) / 3_600_000;
}

export function AssetCoveragePanel({ coverage }: { coverage: AssetCoverageRow[] }) {
  if (!coverage.length) {
    // The ledger is not deployed (no worker running with --assets-db-path).
    // Render nothing rather than an empty table claiming zero coverage.
    return null;
  }

  return (
    <section className="bg-[#11100c] border-b border-[#2a2618] py-2">
      <div className="px-4 text-[#d4a017] font-bold tracking-widest text-base">
        ASSET LEDGER FRESHNESS
      </div>
      <table className="w-full text-sm font-mono mt-1">
        <thead>
          <tr className="text-[13px] uppercase tracking-widest text-[#8a8570]">
            <th className="text-left px-4 font-normal">source</th>
            <th className="text-right px-4 font-normal">known</th>
            <th className="text-right px-4 font-normal">stale</th>
            <th className="text-right px-4 font-normal">oldest</th>
          </tr>
        </thead>
        <tbody>
          {coverage.map((row) => {
            const age = ageHours(row.oldest);
            const share = row.agents > 0 ? row.stale / row.agents : 0;
            // Three states, so a straggler is visible without crying stall.
            const tone =
              row.stale === 0
                ? 'text-[#d4a017]'
                : share >= STALL_SHARE
                  ? 'text-red-400'
                  : 'text-orange-400';

            return (
              <tr key={row.source} className={tone}>
                <td className="text-left px-4">{row.source}</td>
                <td className="text-right px-4">
                  {row.agents} {COUNT_LABEL[row.source] ?? 'agents'}
                </td>
                <td className="text-right px-4">
                  {row.stale > 0 ? `${row.stale}/${row.agents}` : '0'}
                </td>
                <td className="text-right px-4">{age === null ? '—' : `${age.toFixed(1)}h`}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </section>
  );
}
