import { useFleetStream } from '../../lib/useFleetStream';
import { AccountingStrip } from './AccountingStrip';

export function OvermindPage() {
  const stream = useFleetStream();
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
          {/* Task 8: fleet rail */}
        </div>
      </div>
    </div>
  );
}
