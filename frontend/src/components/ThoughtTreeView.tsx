import { useEffect, useRef } from 'react'
import {
  ReactFlow,
  Background,
  useNodesState,
  useEdgesState,
  type Node,
  type Edge,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { RadarChart } from './RadarChart'

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
  status: 'pending' | 'evaluating' | 'active' | 'pruned' | 'winner'
  children?: ThoughtNodeData[]
  depth: number
  eval_time_ms: number
  prompt?: string
  raw_response?: string
}

interface ThoughtTree {
  id: string
  agent_id: string
  situation: string
  stage: string
  root: ThoughtNodeData[]
  winner_id: string
  duration_ms: number
  model: string
  weights: AxisScores
}

interface ThoughtTreeViewProps {
  tree: ThoughtTree | null
  onNodeClick?: (node: ThoughtNodeData) => void
}

const NODE_WIDTH = 260
const NODE_HEIGHT = 220
const H_GAP = 48
const V_GAP = 100

function statusBorderClass(status: ThoughtNodeData['status']): string {
  switch (status) {
    case 'winner':
      return 'border-green-400 shadow-lg shadow-green-900/50'
    case 'pruned':
      return 'border-gray-600 opacity-40 transition-opacity duration-500'
    case 'pending':
      return 'border-gray-600 border-dashed'
    case 'evaluating':
      return 'border-yellow-500 animate-pulse'
    default: // active
      return 'border-blue-500'
  }
}

function edgeColor(status: ThoughtNodeData['status']): string {
  switch (status) {
    case 'winner':
      return '#4ade80'
    case 'pruned':
      return '#4b5563'
    default:
      return '#3b82f6'
  }
}

function buildNodesAndEdges(
  tree: ThoughtTree,
  onNodeClick?: (node: ThoughtNodeData) => void,
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = []
  const edges: Edge[] = []

  // Root situation node at top center
  const totalBranchWidth = tree.root.length * (NODE_WIDTH + H_GAP) - H_GAP
  const rootX = totalBranchWidth / 2 - 80
  const rootY = 20

  nodes.push({
    id: 'root',
    position: { x: rootX, y: rootY },
    data: { label: tree.situation },
    style: {
      background: '#111827',
      border: '1px solid #374151',
      borderRadius: '8px',
      color: '#d1d5db',
      padding: '10px 16px',
      fontSize: '12px',
      maxWidth: '320px',
      textAlign: 'center' as const,
    },
  })

  // Branch nodes
  tree.root.forEach((branch, bi) => {
    const branchX = bi * (NODE_WIDTH + H_GAP)
    const branchY = rootY + 120

    const borderClass = statusBorderClass(branch.status)
    const isWinner = branch.status === 'winner'

    nodes.push({
      id: branch.id,
      position: { x: branchX, y: branchY },
      data: {
        label: (
          <div
            className={`bg-gray-800 border-2 rounded-lg p-3 cursor-pointer ${borderClass}`}
            style={{ width: NODE_WIDTH, minHeight: NODE_HEIGHT }}
            onClick={() => onNodeClick?.(branch)}
          >
            <div className="flex items-center gap-1 mb-1">
              <span className="text-sm font-bold text-white truncate flex-1">
                {branch.action}
              </span>
              {branch.status === 'pending' ? (
                <span className="text-xs px-1.5 py-0.5 rounded bg-gray-700 text-gray-500">—</span>
              ) : branch.status === 'evaluating' ? (
                <span className="text-xs px-1.5 py-0.5 rounded bg-yellow-800 text-yellow-300 animate-pulse">...</span>
              ) : (
                <span className={`text-sm font-bold px-1.5 py-0.5 rounded ${isWinner ? 'bg-green-700 text-green-200' : 'bg-gray-700 text-gray-300'}`}>
                  {Math.round(branch.combined)}
                </span>
              )}
            </div>
            {branch.target && (
              <div className="text-xs text-gray-400 truncate mb-2">→ {branch.target}</div>
            )}
            {(branch.status === 'active' || branch.status === 'winner' || branch.status === 'pruned') && (
              <>
                <div className="flex justify-center mb-2">
                  <RadarChart scores={branch.scores} size={90} />
                </div>
                <p className="text-xs text-gray-400 leading-relaxed">
                  {branch.reasoning}
                </p>
              </>
            )}
            {branch.status === 'evaluating' && (
              <div className="flex items-center justify-center h-24 text-xs text-yellow-400 animate-pulse">
                Evaluating...
              </div>
            )}
            {branch.status === 'pending' && (
              <div className="flex items-center justify-center h-24 text-xs text-gray-500">
                Waiting...
              </div>
            )}
          </div>
        ),
      },
      style: { padding: 0, background: 'transparent', border: 'none' },
    })

    edges.push({
      id: `root-${branch.id}`,
      source: 'root',
      target: branch.id,
      animated: isWinner,
      style: {
        stroke: edgeColor(branch.status),
        strokeWidth: isWinner ? 2 : 1,
      },
    })

    // Planned steps — rendered as a single vertical container node
    if (branch.children && branch.children.length > 0) {
      const planId = `${branch.id}_plan`
      const planX = branchX
      const planY = branchY + NODE_HEIGHT + V_GAP
      const isWinnerPlan = branch.status === 'winner'
      const planBorder = isWinnerPlan
        ? 'border-green-600/50'
        : branch.status === 'pruned'
          ? 'border-gray-700 opacity-40'
          : 'border-blue-800/50'

      nodes.push({
        id: planId,
        position: { x: planX, y: planY },
        data: {
          label: (
            <div
              className={`bg-gray-900/80 border rounded-lg p-3 ${planBorder}`}
              style={{ width: NODE_WIDTH }}
            >
              <div className="text-[10px] font-semibold uppercase tracking-wider text-gray-500 mb-2">
                Planned Steps
              </div>
              <div className="space-y-1.5">
                {branch.children.map((child, ci) => (
                  <div
                    key={child.id}
                    className={`flex items-center gap-2 px-2 py-1.5 rounded cursor-pointer ${
                      isWinnerPlan
                        ? 'bg-green-900/30 hover:bg-green-900/50'
                        : 'bg-gray-800/60 hover:bg-gray-800'
                    }`}
                    onClick={() => onNodeClick?.(child)}
                  >
                    <span className={`text-[10px] font-mono w-4 text-center ${
                      isWinnerPlan ? 'text-green-500' : 'text-gray-600'
                    }`}>
                      {ci + 1}
                    </span>
                    <span className="text-xs font-semibold text-white truncate">
                      {child.action}
                    </span>
                    {child.target && (
                      <span className="text-[10px] text-gray-400 truncate">
                        → {child.target}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          ),
        },
        style: { padding: 0, background: 'transparent', border: 'none' },
      })

      edges.push({
        id: `${branch.id}-${planId}`,
        source: branch.id,
        target: planId,
        animated: isWinnerPlan,
        style: {
          stroke: edgeColor(branch.status),
          strokeWidth: isWinnerPlan ? 2 : 1,
          strokeDasharray: '4 4',
        },
      })
    }
  })

  return { nodes, edges }
}

export const ThoughtTreeView: React.FC<ThoughtTreeViewProps> = ({ tree, onNodeClick }) => {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])
  const lastTreeIdRef = useRef<string | null>(null)
  const reactFlowRef = useRef<{ fitView: () => void } | null>(null)

  useEffect(() => {
    if (!tree) return
    const { nodes: n, edges: e } = buildNodesAndEdges(tree, onNodeClick)
    setNodes(n)
    setEdges(e)

    // Only fitView when a new tree starts (different ID)
    if (tree.id !== lastTreeIdRef.current) {
      lastTreeIdRef.current = tree.id
      // Small delay to let React Flow render nodes before fitting
      setTimeout(() => reactFlowRef.current?.fitView(), 50)
    }
  }, [tree, onNodeClick, setNodes, setEdges])

  if (!tree) {
    return (
      <div className="flex items-center justify-center h-64 text-gray-500 text-sm">
        Waiting for thought tree...
      </div>
    )
  }

  return (
    <div className="flex flex-col bg-gray-950 rounded-lg overflow-hidden border border-gray-800" style={{ height: '600px' }}>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-gray-800 bg-gray-900 flex-shrink-0">
        <span className="text-xs font-semibold text-gray-300 uppercase tracking-wider">
          Thought Engine
        </span>
        <div className="flex items-center gap-3 text-xs text-gray-500">
          {tree.stage && tree.stage !== 'complete' && (
            <span className="bg-yellow-900 text-yellow-300 px-2 py-0.5 rounded animate-pulse">
              {tree.stage}
            </span>
          )}
          {tree.stage === 'complete' && (
            <span className="bg-green-900 text-green-300 px-2 py-0.5 rounded">done</span>
          )}
          {tree.model && (
            <span className="bg-gray-800 px-2 py-0.5 rounded text-gray-400">{tree.model}</span>
          )}
          {tree.stage === 'complete' && tree.duration_ms != null && (
            <span>{(tree.duration_ms / 1_000_000_000).toFixed(1)}s</span>
          )}
        </div>
      </div>

      {/* React Flow canvas */}
      <div className="flex-1 min-h-0">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onInit={(instance) => { reactFlowRef.current = instance; instance.fitView({ padding: 0.2 }) }}
          nodesDraggable={true}
          nodesConnectable={false}
          elementsSelectable={true}
          colorMode="dark"
        >
          <Background color="#1f2937" gap={20} />
        </ReactFlow>
      </div>
    </div>
  )
}
