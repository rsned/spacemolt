import { useMemo, useState } from 'react';
import { useFleetStream } from '../../lib/useFleetStream';
import { AccountingStrip } from './AccountingStrip';
import { FleetRail } from './FleetRail';

export function OvermindPage() {
  const stream = useFleetStream();
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  // stream.agents (a Map) gets a fresh identity on every snapshot/delta event
  // (see useFleetStream.ts), so this recomputes exactly when the data changes —
  // not on every OvermindPage render (e.g. selection clicks) — preserving
  // FleetRail's own useMemo/AgentCard memoization downstream.
  const agents = useMemo(() => [...stream.agents.values()], [stream.agents]);
  return (
    <div className="h-full flex flex-col bg-[#0a0a08] text-[#d8d3c0]">
      <AccountingStrip
        accounting={stream.accounting}
        agentCount={stream.agents.size}
        staleFleets={stream.staleFleets}
        connected={stream.connected}
      />
      <div className="flex-1 flex min-h-0">
        <div className="flex-1 min-w-0" id="ov-map-slot">
          {/* Task 9: galaxy map + fleet overlay */}
          <div className="h-full grid place-items-center text-[#8a8570]">map pending</div>
        </div>
        <div className="w-80 border-l border-[#2a2618] overflow-y-auto" id="ov-rail-slot">
          <FleetRail
            agents={agents}
            offMap={stream.offMap}
            staleFleets={stream.staleFleets}
            selectedId={selectedAgent}
            onSelect={setSelectedAgent}
          />
        </div>
      </div>
    </div>
  );
}
