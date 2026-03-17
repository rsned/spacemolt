import { useState } from 'react'
import { ThoughtTreeView } from './ThoughtTreeView'
import { DebugPanel } from './DebugPanel'
import { useThoughtEngine } from '../lib/useThoughtEngine'
import type { ThoughtNodeData, ThoughtTree } from '../lib/useThoughtEngine'

interface ThoughtEnginePageProps {
  agentId: string | null
}

export function ThoughtEnginePage({ agentId }: ThoughtEnginePageProps) {
  const { currentTree, history, connected } = useThoughtEngine(agentId)
  const [selectedNode, setSelectedNode] = useState<ThoughtNodeData | null>(null)
  const [viewingTree, setViewingTree] = useState<ThoughtTree | null>(null)

  const displayTree = viewingTree || currentTree

  return (
    <div className="space-y-4">
      {/* Status bar */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold text-gray-200">Thought Engine</h2>
          <span className={`text-xs px-2 py-0.5 rounded ${
            connected ? 'bg-green-900 text-green-400' : 'bg-gray-700 text-gray-400'
          }`}>
            {connected ? 'STREAMING' : agentId ? 'CONNECTING...' : 'NO AGENT'}
          </span>
          {agentId && (
            <span className="text-xs text-gray-500">Agent: {agentId}</span>
          )}
        </div>

        {/* History selector */}
        {history.length > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-gray-500">History ({history.length}):</span>
            <div className="flex gap-1">
              <button
                onClick={() => { setViewingTree(null); setSelectedNode(null) }}
                className={`px-2 py-0.5 text-xs rounded ${
                  !viewingTree ? 'bg-cyan-700 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                }`}
              >
                Live
              </button>
              {history.slice(0, 10).map((tree, i) => (
                <button
                  key={tree.id}
                  onClick={() => { setViewingTree(tree); setSelectedNode(null) }}
                  className={`px-2 py-0.5 text-xs rounded ${
                    viewingTree?.id === tree.id ? 'bg-cyan-700 text-white' : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
                  }`}
                  title={new Date(tree.timestamp).toLocaleTimeString()}
                >
                  {i === 0 ? 'Prev' : `-${i + 1}`}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {!agentId ? (
        <div className="flex items-center justify-center h-96 text-gray-500 bg-gray-950 rounded-lg border border-gray-800">
          Subscribe to an agent on the Home tab to see their thought process
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
