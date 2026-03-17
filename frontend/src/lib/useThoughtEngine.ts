import { useState, useEffect, useCallback, useRef } from 'react'

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
  timestamp: string
  situation: string
  root: ThoughtNodeData[]
  winner_id: string
  duration_ms: number
  model: string
  weights: AxisScores
}

interface SSEEvent {
  agent_id: string
  type: string
  timestamp: string
  data: unknown
}

interface UseThoughtEngineResult {
  currentTree: ThoughtTree | null
  history: ThoughtTree[]
  connected: boolean
}

const MAX_HISTORY = 20

/**
 * Hook that connects to the agent's SSE event stream and captures
 * thought_tree events for visualization.
 */
export function useThoughtEngine(agentId: string | null, apiBaseUrl?: string): UseThoughtEngineResult {
  const [currentTree, setCurrentTree] = useState<ThoughtTree | null>(null)
  const [history, setHistory] = useState<ThoughtTree[]>([])
  const [connected, setConnected] = useState(false)
  const eventSourceRef = useRef<EventSource | null>(null)

  const baseUrl = apiBaseUrl || `${window.location.protocol}//${window.location.host}`

  const connect = useCallback((agent: string) => {
    // Close existing connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }

    const url = `${baseUrl}/api/agents/${agent}/stream`
    const es = new EventSource(url)
    eventSourceRef.current = es

    es.onopen = () => setConnected(true)
    es.onerror = () => {
      // onerror fires on reconnect attempts too; only mark disconnected if closed
      if (es.readyState === EventSource.CLOSED) {
        setConnected(false)
      }
    }

    // Listen for the 'connected' named event
    es.addEventListener('connected', () => setConnected(true))

    // Listen for 'thought_tree' named event
    es.addEventListener('thought_tree', (event) => {
      try {
        const parsed: SSEEvent = JSON.parse(event.data)
        const tree = (parsed.data ?? parsed) as ThoughtTree
        setCurrentTree(tree)
        setHistory(prev => [tree, ...prev].slice(0, MAX_HISTORY))
      } catch {
        // ignore parse errors
      }
    })

    // Also listen for unnamed messages as fallback
    es.onmessage = (event) => {
      try {
        const parsed: SSEEvent = JSON.parse(event.data)
        if (parsed.type === 'thought_tree' && parsed.data) {
          const tree = parsed.data as ThoughtTree
          setCurrentTree(tree)
          setHistory(prev => [tree, ...prev].slice(0, MAX_HISTORY))
        }
      } catch {
        // ignore parse errors
      }
    }
  }, [baseUrl])

  useEffect(() => {
    if (agentId) {
      connect(agentId)
    } else {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
        eventSourceRef.current = null
      }
      setConnected(false)
    }

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
        eventSourceRef.current = null
      }
    }
  }, [agentId, connect])

  return { currentTree, history, connected }
}

export type { ThoughtTree, ThoughtNodeData, AxisScores }
