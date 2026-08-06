import type { AssetCoverageRow } from '../../lib/useFleetStream';

// Per-source cadences, in hours. A source is flagged once it exceeds 2x its
// cadence: one missed boundary is churn, two is a stall worth looking at.
// MUST match pkg/assets/coverage.go's CoverageCadence -- that map drives the
// `stale` column's own 2x-cadence cutoff, and the two have to agree or a red
// row and a `stale` count of 0 can both be "correct" for the same row.
const CADENCE_HOURS: Record<string, number> = {
  agent_profile: 1,
  agent_carrier: 1,
  agent_hulls: 1,
  agent_skills: 1,
  agent_storage: 24,
  faction_storage: 24,
};

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
      <div className="px-4 text-[#d4a017] font-bold tracking-widest text-sm">
        ASSET LEDGER FRESHNESS
      </div>
      <table className="w-full text-xs font-mono mt-1">
        <thead>
          <tr className="text-[10px] uppercase tracking-widest text-[#8a8570]">
            <th className="text-left px-4 font-normal">source</th>
            <th className="text-right px-4 font-normal">known</th>
            <th className="text-right px-4 font-normal">stale</th>
            <th className="text-right px-4 font-normal">oldest</th>
          </tr>
        </thead>
        <tbody>
          {coverage.map((row) => {
            const cadence = CADENCE_HOURS[row.source] ?? 24;
            const age = ageHours(row.oldest);
            const alarm = row.stale > 0 || (age !== null && age > cadence * 2);

            return (
              <tr key={row.source} className={alarm ? 'text-red-400' : 'text-[#d4a017]'}>
                <td className="text-left px-4">{row.source}</td>
                <td className="text-right px-4">
                  {row.agents} {COUNT_LABEL[row.source] ?? 'agents'}
                </td>
                <td className="text-right px-4">{row.stale}</td>
                <td className="text-right px-4">{age === null ? '—' : `${age.toFixed(1)}h`}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </section>
  );
}
