import { useCallback, useEffect, useMemo, useState } from 'react';
import { GalaxyMap } from '../galaxy/GalaxyMap';
import { useGalaxyMap } from '../../lib/useGalaxyMap';
import { FLEETS, useFleetStream, type AgentState } from '../../lib/useFleetStream';
import { removeAgent, readdAgent } from '../../lib/fleetAdmin';
import { AccountingStrip } from './AccountingStrip';
import { AssetCoveragePanel } from './AssetCoveragePanel';
import { FleetRail } from './FleetRail';
import { FleetOverlay } from './FleetOverlay';
import { SystemView } from './SystemView';

type OvView = { kind: 'galaxy' } | { kind: 'system'; systemId: string };

export function OvermindPage() {
  const stream = useFleetStream();
  const galaxy = useGalaxyMap('/api/overmind');
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [view, setView] = useState<OvView>({ kind: 'galaxy' });
  const [highlightedIds, setHighlightedIds] = useState<ReadonlySet<string>>(new Set());
  const [visibleFleets, setVisibleFleets] = useState(new Set(Object.keys(FLEETS)));
  // stream.agents (a Map) gets a fresh identity on every snapshot/delta event
  // (see useFleetStream.ts), so this recomputes exactly when the data changes —
  // not on every OvermindPage render (e.g. selection clicks) — preserving
  // FleetRail's own useMemo/AgentCard memoization downstream.
  const agents = useMemo(() => [...stream.agents.values()], [stream.agents]);

  // Escape returns to the galaxy; clicking empty space never does.
  // Exiting must also clear the hover highlight: Escape doesn't move the
  // mouse, so SystemView's mouseleave never fires and the rail's cyan
  // rings would otherwise persist into the galaxy view.
  const closeSystemView = () => {
    setView({ kind: 'galaxy' });
    setHighlightedIds(new Set());
  };
  useEffect(() => {
    if (view.kind !== 'system') return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeSystemView();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [view.kind]);

  const viewedSystem = view.kind === 'system'
    ? galaxy?.systems.find((s) => s.id === view.systemId) ?? null
    : null;
  const systemAgents = useMemo(
    () => (viewedSystem ? agents.filter((a) => a.system_id === viewedSystem.id) : []),
    [agents, viewedSystem],
  );

  const handleRemove = useCallback(async (agent: AgentState) => {
    if (!window.confirm(`Remove ${agent.agent_id} from the ${agent.fleet} fleet?\nIt will drain, stop, and stay out until re-added.`)) return;
    try {
      const res = await removeAgent(agent.fleet, agent.agent_id);
      if (res.status !== 'accepted') alert(`${agent.agent_id}: ${res.status}${res.detail ? ` — ${res.detail}` : ''}`);
    } catch (err) {
      alert(String(err));
    }
  }, []);

  const handleReadd = useCallback(async (fleet: string, agentId: string) => {
    try {
      const res = await readdAgent(fleet, agentId);
      if (res.status !== 'accepted') alert(`${agentId}: ${res.status}${res.detail ? ` — ${res.detail}` : ''}`);
    } catch (err) {
      alert(String(err));
    }
  }, []);

  return (
    <div className="h-full flex flex-col bg-[#0a0a08] text-[#d8d3c0]">
      <AccountingStrip
        accounting={stream.accounting}
        agentCount={stream.agents.size}
        staleFleets={stream.staleFleets}
        connected={stream.connected}
        currentOvermind={stream.currentOvermind}
        currentWorker={stream.currentWorker}
      />
      <AssetCoveragePanel coverage={stream.assetCoverage} />
      <div className="flex-1 flex min-h-0">
        <div className="flex-1 min-w-0 relative flex flex-col" id="ov-map-slot">
          {/* Fleet layer toggles + off-map tray, in normal document flow above the
              map — NOT absolutely positioned over it. GalaxyMap already claims its
              own top-left (search) and top-right (EMPIRES legend) corners with
              absolutely-positioned panels; stacking our controls there collided
              with them (confirmed live). Living in the flex column above the map
              keeps both fully visible and clickable. */}
          <div className="flex items-center gap-2 px-3 py-1.5 border-b border-[#2a2618] bg-[#0a0a08] flex-wrap">
            <span className="text-[10px] uppercase tracking-widest text-[#8a8570]">fleets</span>
            {Object.entries(FLEETS).map(([fleet, color]) => (
              <button key={fleet}
                onClick={() => setVisibleFleets((v) => {
                  const next = new Set(v);
                  if (next.has(fleet)) { next.delete(fleet); } else { next.add(fleet); }
                  return next;
                })}
                className={`px-2 py-0.5 text-[10px] uppercase tracking-widest border rounded-sm
                  ${visibleFleets.has(fleet) ? '' : 'opacity-40'}`}
                style={{ color, borderColor: color }}>
                {fleet}
              </button>
            ))}
            {stream.offMap.length > 0 && (
              <span className="ml-auto flex items-center gap-2 text-[10px] uppercase tracking-widest text-[#8a8570]">
                off-map:
                {stream.offMap.map((a) => (
                  <span key={a.agent_id} style={{ color: FLEETS[a.fleet] }}>
                    {a.agent_id} ({a.system_name})
                  </span>
                ))}
              </span>
            )}
          </div>
          <div className="flex-1 min-h-0 relative">
            {/* GalaxyMap stays mounted while zoomed in so its pan/zoom and
                fetched data survive the round trip; `hidden` removes it from
                layout without unmounting. */}
            <div className={view.kind === 'system' ? 'hidden' : 'absolute inset-0'}>
              <GalaxyMap
                systems={galaxy?.systems}
                onSystemClick={(s) => setView({ kind: 'system', systemId: s.id })}
                hideInfoPanel
                overlay={(project) => (
                  <FleetOverlay
                    agents={agents}
                    moves={stream.moves}
                    systems={galaxy?.systems ?? []}
                    project={project}
                    visibleFleets={visibleFleets}
                    selectedId={selectedAgent}
                    onAgentClick={setSelectedAgent}
                  />
                )}
              />
            </div>
            {viewedSystem && (
              <SystemView
                system={viewedSystem}
                systems={galaxy?.systems ?? []}
                agents={systemAgents}
                moves={stream.moves}
                selectedId={selectedAgent}
                onAgentClick={setSelectedAgent}
                onHoverAgents={(ids) => setHighlightedIds(new Set(ids))}
                onClose={closeSystemView}
              />
            )}
          </div>
        </div>
        <div className="w-80 border-l border-[#2a2618] overflow-y-auto" id="ov-rail-slot">
          <FleetRail
            agents={agents}
            offMap={stream.offMap}
            staleFleets={stream.staleFleets}
            selectedId={selectedAgent}
            onSelect={setSelectedAgent}
            highlightedIds={highlightedIds}
            removed={stream.removed}
            onRemove={handleRemove}
            onReadd={handleReadd}
            overminds={stream.overminds}
          />
        </div>
      </div>
    </div>
  );
}
