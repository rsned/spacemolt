import { useState, useEffect } from 'react'
import { ThoughtTreeView } from './ThoughtTreeView'
import { DebugPanel } from './DebugPanel'
import { useThoughtEngine } from '../lib/useThoughtEngine'
import type { ThoughtNodeData, ThoughtTree } from '../lib/useThoughtEngine'

interface AgentInfo {
  id: string
  username: string
  connected: boolean
  system?: string
  poi?: string
  docked?: boolean
}

interface ThoughtEnginePageProps {
  agentId?: string | null
}

export function ThoughtEnginePage({ agentId: externalAgentId }: ThoughtEnginePageProps) {
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [selectedAgent, setSelectedAgent] = useState<string | null>(externalAgentId ?? null)
  const [loadingAgents, setLoadingAgents] = useState(false)
  const [currentModel, setCurrentModel] = useState<string>('')
  const [availableModels, setAvailableModels] = useState<string[]>([])

  const activeAgent = externalAgentId ?? selectedAgent
  const { currentTree, history, connected } = useThoughtEngine(activeAgent)
  const [selectedNode, setSelectedNode] = useState<ThoughtNodeData | null>(null)
  const [viewingTree, setViewingTree] = useState<ThoughtTree | null>(null)

  const displayTree = viewingTree || currentTree

  // Fetch model info from API
  useEffect(() => {
    const fetchModel = async () => {
      try {
        const resp = await fetch('/api/model')
        if (resp.ok) {
          const data = await resp.json()
          setCurrentModel(data.current ?? '')
          setAvailableModels(data.available ?? [])
        }
      } catch { /* API not available */ }
    }
    fetchModel()
    const interval = setInterval(fetchModel, 30000)
    return () => clearInterval(interval)
  }, [])

  const handleModelChange = async (model: string) => {
    try {
      const resp = await fetch('/api/model', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model }),
      })
      if (resp.ok) {
        const data = await resp.json()
        setCurrentModel(data.current)
      }
    } catch { /* ignore */ }
  }

  // Fetch agents from REST API (standalone mode)
  useEffect(() => {
    if (externalAgentId) return // skip if agent provided externally

    const fetchAgents = async () => {
      setLoadingAgents(true)
      try {
        const resp = await fetch('/api/agents')
        if (resp.ok) {
          const data = await resp.json()
          const list: AgentInfo[] = Array.isArray(data) ? data : (data.agents ?? [])
          setAgents(list)
          // Auto-select first agent if none selected
          if (!selectedAgent && list.length > 0) {
            setSelectedAgent(list[0].id ?? list[0].username)
          }
        }
      } catch {
        // API not available
      } finally {
        setLoadingAgents(false)
      }
    }

    fetchAgents()
    const interval = setInterval(fetchAgents, 15000)
    return () => clearInterval(interval)
  }, [externalAgentId])

  return (
    <div className="space-y-4">
      {/* Status bar */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold text-gray-200">Thought Engine</h2>
          <span className={`text-xs px-2 py-0.5 rounded ${
            connected ? 'bg-green-900 text-green-400' : 'bg-gray-700 text-gray-400'
          }`}>
            {connected ? 'STREAMING' : activeAgent ? 'CONNECTING...' : 'NO AGENT'}
          </span>
        </div>

        <div className="flex items-center gap-3">
          {/* Model selector */}
          {availableModels.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-xs text-gray-500">Model:</span>
              <select
                value={currentModel}
                onChange={(e) => handleModelChange(e.target.value)}
                className="px-2 py-0.5 text-xs bg-gray-800 border border-gray-700 rounded text-gray-300 cursor-pointer"
              >
                {availableModels.map((m) => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
            </div>
          )}

          {/* Agent selector (standalone mode) */}
          {!externalAgentId && (
            <div className="flex items-center gap-2">
              <span className="text-xs text-gray-500">Agent:</span>
              {loadingAgents && agents.length === 0 ? (
                <span className="text-xs text-gray-500">Loading...</span>
              ) : agents.length > 0 ? (
                <div className="flex gap-1">
                  {agents.map((a) => (
                    <button
                      key={a.id ?? a.username}
                      onClick={() => { setSelectedAgent(a.id ?? a.username); setSelectedNode(null); setViewingTree(null) }}
                      className={`px-2 py-0.5 text-xs rounded ${
                        activeAgent === (a.id ?? a.username)
                          ? 'bg-cyan-700 text-white'
                          : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                      }`}
                    >
                      {a.username ?? a.id}
                      {a.connected === false && <span className="text-red-400 ml-1">(offline)</span>}
                    </button>
                  ))}
                </div>
              ) : (
                <input
                  type="text"
                  placeholder="agent id (e.g. miner-1)"
                  className="px-2 py-1 text-xs bg-gray-800 border border-gray-700 rounded text-gray-300 w-40"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      setSelectedAgent((e.target as HTMLInputElement).value)
                      setSelectedNode(null)
                      setViewingTree(null)
                    }
                  }}
                />
              )}
            </div>
          )}

          {/* History selector — only show completed trees, max 8 */}
          {history.length > 0 && (
            <div className="flex items-center gap-2">
              <span className="text-xs text-gray-500">History:</span>
              <div className="flex gap-1">
                <button
                  onClick={() => { setViewingTree(null); setSelectedNode(null) }}
                  className={`px-2 py-0.5 text-xs rounded ${
                    !viewingTree ? 'bg-cyan-700 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                  }`}
                >
                  Live
                </button>
                {history.slice(0, 8).map((tree, i) => (
                  <button
                    key={`hist-${i}-${tree.id}`}
                    onClick={() => { setViewingTree(tree); setSelectedNode(null) }}
                    className={`px-2 py-0.5 text-xs rounded ${
                      viewingTree?.id === tree.id ? 'bg-cyan-700 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                    }`}
                    title={tree.situation || tree.id}
                  >
                    -{i + 1}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

      {!activeAgent ? (
        <div className="flex items-center justify-center h-96 text-gray-500 bg-gray-950 rounded-lg border border-gray-800">
          {loadingAgents ? 'Loading agents...' : 'Select an agent above or enter an agent ID to watch their thought process'}
        </div>
      ) : (
        <>
          {/* Tree visualization */}
          <ThoughtTreeView
            tree={displayTree}
            onNodeClick={(node) => setSelectedNode(node)}
          />

          {/* Debug panel */}
          <DebugPanel
            node={selectedNode}
            weights={displayTree?.weights}
          />
        </>
      )}
    </div>
  )
}
