import type { GalaxySystem } from '../../lib/useGalaxyMap';
import { FLEETS, type AgentState } from '../../lib/useFleetStream';

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex justify-between border-b border-[#2a2618] py-1 text-xs">
      <span className="uppercase tracking-widest text-[#8a8570]">{k}</span>
      <span className="font-mono text-[#d8d3c0]">{v}</span>
    </div>
  );
}

export function SystemPanel({ system, agents, onClose }: {
  system: GalaxySystem; agents: AgentState[]; onClose: () => void;
}) {
  // Anchored bottom-right: GalaxyMap's own EMPIRES legend already claims the
  // map's top-right corner, so top-right would collide with it.
  return (
    <div className="absolute bottom-3 right-3 w-64 max-h-[70%] overflow-y-auto bg-[#11100c] border border-[#2a2618] rounded-sm p-3 shadow-lg">
      <div className="flex justify-between items-center mb-2">
        <span className="text-[#d4a017] font-bold tracking-widest text-sm uppercase">{system.name}</span>
        <button onClick={onClose} className="text-[#8a8570] hover:text-[#d8d3c0]">✕</button>
      </div>
      <Row k="empire" v={system.empire || 'neutral'} />
      <Row k="police" v={`${system.police_level}`} />
      <Row k="jump lanes" v={`${system.connections.length}`} />
      {system.is_stronghold && <Row k="warning" v="PIRATE STRONGHOLD" />}
      <div className="mt-2 text-[10px] uppercase tracking-widest text-[#8a8570]">agents here</div>
      {agents.length === 0 && <div className="text-xs text-[#8a8570] py-1">none</div>}
      {agents.map((a) => (
        <div key={a.agent_id} className="flex justify-between text-xs py-0.5">
          <span style={{ color: FLEETS[a.fleet] }}>{a.agent_id}</span>
          <span className="text-[#8a8570]">{a.poi}{a.docked ? ' ⚓' : ''}</span>
        </div>
      ))}
    </div>
  );
}
