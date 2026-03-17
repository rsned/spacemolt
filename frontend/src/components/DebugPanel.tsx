import { useState } from 'react'

interface AxisScores {
  survival: number
  profit: number
  goal_progress: number
  risk: number
  efficiency: number
}

interface ThoughtNodeData {
  id: string
  action: string
  target: string
  reasoning: string
  scores: AxisScores
  combined: number
  status: 'active' | 'pruned' | 'winner'
  depth: number
  eval_time_ms: number
  prompt?: string
  raw_response?: string
}

interface DebugPanelProps {
  node: ThoughtNodeData | null
  weights?: AxisScores
}

const AXIS_LABELS: Record<keyof AxisScores, string> = {
  survival: 'Survival',
  profit: 'Profit',
  goal_progress: 'Goal Progress',
  risk: 'Risk',
  efficiency: 'Efficiency',
}

const AXES = ['survival', 'profit', 'goal_progress', 'risk', 'efficiency'] as const

type TabId = 'scores' | 'prompt' | 'response'

export const DebugPanel: React.FC<DebugPanelProps> = ({ node, weights }) => {
  const [expanded, setExpanded] = useState(false)
  const [activeTab, setActiveTab] = useState<TabId>('scores')

  if (!node) return null

  const label = node.target ? `${node.action} → ${node.target}` : node.action

  return (
    <div className="bg-gray-900 border border-gray-700 rounded-lg overflow-hidden text-xs">
      {/* Collapsed header */}
      <button
        className="w-full flex items-center justify-between px-3 py-2 hover:bg-gray-800 transition-colors text-left"
        onClick={() => setExpanded((v) => !v)}
      >
        <span className="text-gray-400 truncate">
          <span className="text-gray-500 mr-1">Debug:</span>
          <span className="text-gray-200">{label}</span>
        </span>
        <span className="text-gray-500 ml-2 flex-shrink-0">{expanded ? '▲' : '▼'}</span>
      </button>

      {expanded && (
        <div className="border-t border-gray-700">
          {/* Tabs */}
          <div className="flex border-b border-gray-700">
            {(['scores', 'prompt', 'response'] as TabId[]).map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={`px-3 py-1.5 capitalize transition-colors ${
                  tab === activeTab
                    ? 'bg-gray-800 text-white border-b-2 border-blue-500'
                    : 'text-gray-500 hover:text-gray-300 hover:bg-gray-800'
                }`}
              >
                {tab}
              </button>
            ))}
          </div>

          {/* Tab content */}
          <div className="p-3">
            {activeTab === 'scores' && (
              <div>
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-gray-500 border-b border-gray-700">
                      <th className="text-left py-1 font-normal">Axis</th>
                      <th className="text-right py-1 font-normal">Score</th>
                      <th className="text-right py-1 font-normal">Weight</th>
                      <th className="text-right py-1 font-normal">Weighted</th>
                    </tr>
                  </thead>
                  <tbody>
                    {AXES.map((axis) => {
                      const score = node.scores[axis] ?? 0
                      const weight = weights?.[axis] ?? 1
                      const weighted = (score * weight) / 100
                      return (
                        <tr key={axis} className="border-b border-gray-800 text-gray-300">
                          <td className="py-1 text-gray-400">{AXIS_LABELS[axis]}</td>
                          <td className="py-1 text-right">{score.toFixed(1)}</td>
                          <td className="py-1 text-right text-gray-500">{weight.toFixed(2)}</td>
                          <td className="py-1 text-right text-blue-400">{weighted.toFixed(2)}</td>
                        </tr>
                      )
                    })}
                  </tbody>
                  <tfoot>
                    <tr className="text-gray-300 font-semibold">
                      <td className="pt-2" colSpan={3}>
                        Combined
                      </td>
                      <td className="pt-2 text-right text-green-400">
                        {node.combined.toFixed(1)}
                      </td>
                    </tr>
                  </tfoot>
                </table>
                <div className="mt-2 text-gray-500 text-xs">
                  Eval time: {(node.eval_time_ms / 1_000_000_000).toFixed(1)}s · Depth: {node.depth} · Status:{' '}
                  <span
                    className={
                      node.status === 'winner'
                        ? 'text-green-400'
                        : node.status === 'pruned'
                          ? 'text-gray-500'
                          : 'text-blue-400'
                    }
                  >
                    {node.status}
                  </span>
                </div>
              </div>
            )}

            {activeTab === 'prompt' && (
              <pre className="text-gray-300 whitespace-pre-wrap break-words max-h-64 overflow-y-auto text-xs font-mono bg-gray-950 p-2 rounded border border-gray-800">
                {node.prompt ?? <span className="text-gray-600 italic">No prompt available</span>}
              </pre>
            )}

            {activeTab === 'response' && (
              <pre className="text-gray-300 whitespace-pre-wrap break-words max-h-64 overflow-y-auto text-xs font-mono bg-gray-950 p-2 rounded border border-gray-800">
                {node.raw_response ?? (
                  <span className="text-gray-600 italic">No response available</span>
                )}
              </pre>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
