export async function removeAgent(fleet: string, agentId: string): Promise<{ status: string; detail?: string }> {
  const res = await fetch(`/api/overmind/fleets/${encodeURIComponent(fleet)}/agents/${encodeURIComponent(agentId)}/remove`, { method: 'POST' });
  if (!res.ok) throw new Error(`remove failed: ${res.status} ${await res.text()}`);
  return res.json();
}

export async function readdAgent(fleet: string, agentId: string): Promise<{ status: string; detail?: string }> {
  const res = await fetch(`/api/overmind/fleets/${encodeURIComponent(fleet)}/agents/${encodeURIComponent(agentId)}/readd`, { method: 'POST' });
  if (!res.ok) throw new Error(`readd failed: ${res.status} ${await res.text()}`);
  return res.json();
}
