import { useMemo } from 'react';
import { FLEETS } from '../../lib/useFleetStream';
import { useAgentSheet, type AgentSheetData, type SheetSkill, type SheetStanding } from '../../lib/useRoster';

/** Ability-score box: big tabular numeral over a small-caps label, framed
    with a hairline double border. The one deliberately tabletop element. */
function StatBox({ label, value, sub, accent }: {
  label: string; value: string; sub?: string; accent: string;
}) {
  return (
    <div className="min-w-[92px] px-3 py-2 border border-[#2a2618] rounded-sm bg-[#11100c] text-center"
      style={{ boxShadow: `inset 0 0 0 1px #0a0a08, inset 0 0 0 2px ${accent}33` }}>
      <div className="text-lg font-bold tabular-nums leading-tight" style={{ color: accent }}>{value}</div>
      {sub && <div className="text-[10px] tabular-nums text-[#8a8570] leading-tight">{sub}</div>}
      <div className="mt-0.5 text-[9px] uppercase tracking-[0.2em] text-[#8a8570]">{label}</div>
    </div>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2 mb-2">
      <span className="text-[10px] uppercase tracking-[0.25em] text-[#8a8570]">{children}</span>
      <span className="flex-1 border-t border-[#2a2618]" />
    </div>
  );
}

/** N-axis skill radar (the shared RadarChart is fixed to decision scores). */
function SkillRadar({ skills, accent, size = 150 }: { skills: SheetSkill[]; accent: string; size?: number }) {
  const top = skills.slice(0, 6);
  if (top.length < 3) return null;
  const maxLevel = Math.max(...top.map((s) => s.level), 1);
  const cx = size / 2; const cy = size / 2; const r = size / 2 - 24;
  const angle = (i: number) => (Math.PI * 2 * i) / top.length - Math.PI / 2;
  const pt = (i: number, frac: number) =>
    `${cx + Math.cos(angle(i)) * r * frac},${cy + Math.sin(angle(i)) * r * frac}`;
  const rings = [0.33, 0.66, 1];
  const poly = top.map((s, i) => pt(i, s.level / maxLevel)).join(' ');
  return (
    <svg width={size} height={size} className="mx-auto block">
      {rings.map((f) => (
        <polygon key={f} points={top.map((_, i) => pt(i, f)).join(' ')}
          fill="none" stroke="#2a2618" strokeWidth="1" />
      ))}
      {top.map((_, i) => (
        <line key={i} x1={cx} y1={cy}
          x2={cx + Math.cos(angle(i)) * r} y2={cy + Math.sin(angle(i)) * r}
          stroke="#2a2618" strokeWidth="1" />
      ))}
      <polygon points={poly} fill={`${accent}33`} stroke={accent} strokeWidth="1.5" />
      {top.map((s, i) => {
        const lx = cx + Math.cos(angle(i)) * (r + 13);
        const ly = cy + Math.sin(angle(i)) * (r + 13);
        return (
          <text key={s.skill} x={lx} y={ly} textAnchor="middle" dominantBaseline="middle"
            className="fill-[#8a8570]" fontSize="8" style={{ textTransform: 'uppercase', letterSpacing: '0.1em' }}>
            {s.skill.slice(0, 6)}
          </text>
        );
      })}
    </svg>
  );
}

/** Baseline is the decay target (the durable standing); reputation floats
    above it. The ladder shows both so the mechanic is visible, not implied. */
function StandingRow_({ s }: { s: SheetStanding }) {
  const span = 20; // display range clamp; rep beyond ±20 pins to the edge
  const frac = (v: number) => Math.max(0, Math.min(1, (v + span) / (2 * span)));
  const hostile = s.baseline < 0;
  return (
    <div className="mb-1.5">
      <div className="flex justify-between text-[10px]">
        <span className={hostile ? 'text-red-700' : 'text-[#d8d3c0]'}>{s.faction}</span>
        <span className="tabular-nums text-[#8a8570]">
          {s.reputation}{s.baseline !== s.reputation ? ` (base ${s.baseline})` : ''}
          {s.outstanding_bounty > 0 && <span className="text-red-600"> · bounty {s.outstanding_bounty.toLocaleString()}</span>}
          {s.jailed_until && <span className="text-red-600"> · jailed</span>}
        </span>
      </div>
      <div className="relative h-1.5 bg-[#2a2618] rounded-sm">
        <span className="absolute top-0 bottom-0 w-px bg-[#5a5545]" style={{ left: '50%' }} />
        <span className="absolute top-0 bottom-0 w-1 rounded-sm"
          title={`baseline ${s.baseline}`}
          style={{ left: `calc(${frac(s.baseline) * 100}% - 2px)`, background: hostile ? '#dc2626' : '#d4a017' }} />
        <span className="absolute top-0.5 bottom-0.5 w-0.5"
          title={`reputation ${s.reputation}`}
          style={{ left: `calc(${frac(s.reputation) * 100}% - 1px)`, background: '#d8d3c0' }} />
      </div>
    </div>
  );
}

const FEAT_LABELS: Record<string, string> = {
  freight: 'Freight carrier',
  haul: 'Haul eligible',
  mission_delivery: 'Delivery missions',
  smuggling: 'Smuggling',
  stronghold_access: 'Stronghold access',
};

function Feats({ sheet }: { sheet: AgentSheetData }) {
  const entries = Object.entries(sheet.capabilities)
    .sort(([a], [b]) => a.localeCompare(b));
  if (!entries.length) return <div className="text-[10px] text-[#5a5545]">Not evaluated yet.</div>;
  return (
    <div className="space-y-1.5">
      {entries.map(([key, c]) => (
        <div key={key} className={`px-2 py-1.5 border rounded-sm text-[11px] ${
          c.eligible ? 'border-[#d4a017]/60 bg-[#d4a017]/5' : 'border-[#2a2618]'}`}>
          <span className={c.eligible ? 'text-[#d4a017]' : 'text-[#5a5545]'}>
            {c.eligible ? '◆' : '◇'} {FEAT_LABELS[key] ?? key}
          </span>
          {!c.eligible && c.reason && (
            <div className="mt-0.5 text-[10px] text-[#8a8570]">{c.reason}</div>
          )}
        </div>
      ))}
    </div>
  );
}

export function AgentSheet({ agentId, onBack }: { agentId: string; onBack: () => void }) {
  const { data: sheet, error, loading } = useAgentSheet(agentId);
  const accent = useMemo(
    () => (sheet?.fleet ? FLEETS[sheet.fleet] ?? '#d4a017' : '#d4a017'),
    [sheet?.fleet],
  );

  if (loading) return <div className="p-6 text-xs text-[#8a8570]">Pulling the dossier…</div>;
  if (error) return <div className="p-6 text-xs text-red-600">Sheet failed to load: {error}</div>;
  if (!sheet) return <div className="p-6 text-xs text-[#8a8570]">No ledger record for {agentId}.</div>;

  const ship = sheet.ship;
  const capturedAt = sheet.captured_at ? new Date(sheet.captured_at).toLocaleString() : 'never';

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-5xl mx-auto p-4">
        {/* Dossier header */}
        <div className="flex items-baseline gap-3 flex-wrap">
          <button onClick={onBack}
            className="text-[10px] uppercase tracking-widest text-[#8a8570] hover:text-[#d8d3c0] border border-[#2a2618] rounded-sm px-2 py-1">
            ← roster
          </button>
          <h1 className="text-2xl font-bold" style={{ color: accent }}>{sheet.agent_id || sheet.username}</h1>
          <span className="text-sm text-[#8a8570]">{sheet.username}</span>
          {sheet.fleet && (
            <span className="px-1.5 text-[10px] uppercase tracking-widest border rounded-sm self-center"
              style={{ color: accent, borderColor: accent }}>
              {sheet.fleet}{sheet.role && sheet.role !== sheet.fleet ? ` · ${sheet.role}` : ''}
            </span>
          )}
          {sheet.stale && (
            <span className="px-1.5 text-[10px] uppercase tracking-widest border border-red-800 text-red-700 rounded-sm self-center">
              stale
            </span>
          )}
        </div>
        <div className="mt-1 text-[11px] text-[#8a8570]">
          {ship ? `${ship.class_name} — ` : ''}
          {sheet.empire || 'unknown empire'}
          {sheet.faction_id && ` · ${sheet.faction_id}${sheet.faction_rank ? ` (${sheet.faction_rank})` : ''}`}
          {' · '}{sheet.current_system || '?'}{sheet.current_poi ? ` / ${sheet.current_poi}` : ''}
          {sheet.docked_at_base && ' · docked'}
          <span className="text-[#5a5545]"> · captured {capturedAt}</span>
        </div>

        {/* Signature stat-box strip */}
        <div className="mt-4 flex gap-2 flex-wrap">
          <StatBox label="credits" value={Math.round(sheet.credits).toLocaleString()} accent={accent} />
          <StatBox label="experience" value={sheet.experience.toLocaleString()} accent={accent} />
          {ship && <>
            <StatBox label="hull" value={`${ship.hull_current}`} sub={`of ${ship.hull_max}`} accent={accent} />
            <StatBox label="fuel" value={`${ship.fuel_current}`} sub={`of ${ship.fuel_max}`} accent={accent} />
            <StatBox label="cargo" value={`${ship.cargo_used}`}
              sub={ship.cargo_capacity ? `of ${ship.cargo_capacity}` : undefined} accent={accent} />
          </>}
        </div>

        {/* Skills · Reputation · Feats */}
        <div className="mt-5 grid grid-cols-1 md:grid-cols-3 gap-5">
          <div>
            <SectionTitle>skills</SectionTitle>
            {sheet.skills?.length ? <>
              <SkillRadar skills={sheet.skills} accent={accent} />
              <div className="mt-2 space-y-0.5">
                {sheet.skills.map((s) => (
                  <div key={s.skill} className="flex justify-between text-[11px]">
                    <span>{s.skill}</span>
                    <span className="tabular-nums text-[#8a8570]">
                      <span style={{ color: accent }}>{s.level}</span> · {Math.round(s.xp).toLocaleString()} xp
                    </span>
                  </div>
                ))}
              </div>
            </> : <div className="text-[10px] text-[#5a5545]">No skills captured yet.</div>}
          </div>
          <div>
            <SectionTitle>reputation</SectionTitle>
            {sheet.standings?.length
              ? sheet.standings.map((s) => <StandingRow_ key={s.faction} s={s} />)
              : <div className="text-[10px] text-[#5a5545]">No standings captured yet.</div>}
          </div>
          <div>
            <SectionTitle>feats</SectionTitle>
            <Feats sheet={sheet} />
          </div>
        </div>

        {/* Hangar · Storage */}
        <div className="mt-5 grid grid-cols-1 md:grid-cols-2 gap-5 pb-6">
          <div>
            <SectionTitle>hangar</SectionTitle>
            {sheet.hulls?.length ? (
              <table className="w-full text-[11px]">
                <tbody>
                  {sheet.hulls.map((h) => (
                    <tr key={h.ship_id} className="border-b border-[#1a1812]">
                      <td className="py-1 pr-2">
                        <span style={{ color: h.is_active ? accent : undefined }}>
                          {h.is_active ? '▸ ' : ''}{h.class_name}
                        </span>
                        {h.listing_price ? <span className="text-[#8a8570]"> · listed {h.listing_price.toLocaleString()}</span> : null}
                      </td>
                      <td className="py-1 pr-2 tabular-nums text-[#8a8570]">hull {h.hull_current}/{h.hull_max}</td>
                      <td className="py-1 pr-2 tabular-nums text-[#8a8570]">fuel {h.fuel_current}/{h.fuel_max}</td>
                      <td className="py-1 text-[#8a8570]">{h.is_active ? 'active' : h.location || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : <div className="text-[10px] text-[#5a5545]">No hulls captured yet.</div>}
          </div>
          <div>
            <SectionTitle>storage</SectionTitle>
            {sheet.storage?.length ? (
              <table className="w-full text-[11px]">
                <tbody>
                  {sheet.storage.map((s) => (
                    <tr key={s.base_id} className="border-b border-[#1a1812]">
                      <td className="py-1 pr-2">{s.base_id}</td>
                      <td className="py-1 pr-2 tabular-nums text-[#8a8570]">{s.items} items · {Math.round(s.units).toLocaleString()} units</td>
                      <td className="py-1 text-right tabular-nums text-[#8a8570]">{s.credits ? `${s.credits.toLocaleString()} cr` : ''}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : <div className="text-[10px] text-[#5a5545]">Nothing warehoused.</div>}
          </div>
        </div>
      </div>
    </div>
  );
}
