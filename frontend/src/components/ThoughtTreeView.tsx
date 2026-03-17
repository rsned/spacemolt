import { useCallback, useEffect } from 'react'
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
  status: 'active' | 'pruned' | 'winner'
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

const NODE_WIDTH = 200
const NODE_HEIGHT = 160
const CHILD_NODE_HEIGHT = 80
const H_GAP = 24
const V_GAP = 80

function statusBorderClass(status: ThoughtNodeData['status']): string {
  switch (status) {
    case 'winner':
      return 'border-green-400 shadow-lg shadow-green-900/50'
    case 'pruned':
      return 'border-gray-600 opacity-40 transition-opacity duration-500'
    default:
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
    const branchY = rootY + 80

    const borderClass = statusBorderClass(branch.status)
    const isWinner = branch.status === 'winner'

    nodes.push({
      id: branch.id,
      position: { x: branchX, y: branchY },
      data: {
        label: (
          <div
            className={`bg-gray-800 border-2 rounded-lg p-2 cursor-pointer ${borderClass}`}
            style={{ width: NODE_WIDTH, minHeight: NODE_HEIGHT }}
            onClick={() => onNodeClick?.(branch)}
          >
            <div className="flex items-center gap-1 mb-1">
              <span className="text-xs font-semibold text-white truncate flex-1">
                {branch.action}
              </span>
              <span
                className={`text-xs font-bold px-1 rounded ${isWinner ? 'bg-green-700 text-green-200' : 'bg-gray-700 text-gray-300'}`}
              >
                {Math.round(branch.combined)}
              </span>
            </div>
            {branch.target && (
              <div className="text-xs text-gray-400 truncate mb-1">→ {branch.target}</div>
            )}
            <div className="flex gap-2 items-start">
              <RadarChart scores={branch.scores} size={72} />
              <p className="text-xs text-gray-400 leading-tight line-clamp-4 flex-1">
                {branch.reasoning}
              </p>
            </div>
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

    // Child nodes (depth 2)
    if (branch.children && branch.children.length > 0) {
      const totalChildWidth = branch.children.length * (NODE_WIDTH + H_GAP) - H_GAP
      const childStartX = branchX + NODE_WIDTH / 2 - totalChildWidth / 2

      branch.children.forEach((child, ci) => {
        const childX = childStartX + ci * (NODE_WIDTH + H_GAP)
        const childY = branchY + NODE_HEIGHT + V_GAP
        const childBorderClass = statusBorderClass(child.status)
        const childIsWinner = child.status === 'winner'

        nodes.push({
          id: child.id,
          position: { x: childX, y: childY },
          data: {
            label: (
              <div
                className={`bg-gray-800 border-2 rounded-lg p-2 cursor-pointer ${childBorderClass}`}
                style={{ width: NODE_WIDTH, minHeight: CHILD_NODE_HEIGHT }}
                onClick={() => onNodeClick?.(child)}
              >
                <div className="flex items-center gap-1 mb-1">
                  <span className="text-xs font-semibold text-white truncate flex-1">
                    {child.action}
                  </span>
                  <span
                    className={`text-xs font-bold px-1 rounded ${childIsWinner ? 'bg-green-700 text-green-200' : 'bg-gray-700 text-gray-300'}`}
                  >
                    {Math.round(child.combined)}
                  </span>
                </div>
                {child.target && (
                  <div className="text-xs text-gray-400 truncate mb-1">→ {child.target}</div>
                )}
                <p className="text-xs text-gray-500 leading-tight line-clamp-2">
                  {child.reasoning}
                </p>
              </div>
            ),
          },
          style: { padding: 0, background: 'transparent', border: 'none' },
        })

        edges.push({
          id: `${branch.id}-${child.id}`,
          source: branch.id,
          target: child.id,
          animated: childIsWinner,
          style: {
            stroke: edgeColor(child.status),
            strokeWidth: childIsWinner ? 2 : 1,
          },
        })
      })
    }
  })

  return { nodes, edges }
}

export const ThoughtTreeView: React.FC<ThoughtTreeViewProps> = ({ tree, onNodeClick }) => {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])

  const rebuildGraph = useCallback(() => {
    if (!tree) return
    const { nodes: n, edges: e } = buildNodesAndEdges(tree, onNodeClick)
    setNodes(n)
    setEdges(e)
  }, [tree, onNodeClick, setNodes, setEdges])

  useEffect(() => {
    rebuildGraph()
  }, [rebuildGraph])

  if (!tree) {
    return (
      <div className="flex items-center justify-center h-64 text-gray-500 text-sm">
        Waiting for thought tree...
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full bg-gray-950 rounded-lg overflow-hidden border border-gray-800">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-gray-800 bg-gray-900 flex-shrink-0">
        <span className="text-xs font-semibold text-gray-300 uppercase tracking-wider">
          Thought Engine
        </span>
        <div className="flex items-center gap-3 text-xs text-gray-500">
          {tree.model && (
            <span className="bg-gray-800 px-2 py-0.5 rounded text-gray-400">{tree.model}</span>
          )}
          {tree.duration_ms != null && (
            <span>{tree.duration_ms}ms</span>
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
          fitView
          fitViewOptions={{ padding: 0.2 }}
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
