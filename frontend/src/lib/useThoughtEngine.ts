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
  timestamp: string
  situation: string
  stage: string // assessing, evaluating N/M, selecting, complete
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
  paused: boolean
}

const MAX_HISTORY = 10

/**
 * Hook that connects to the agent's SSE event stream and captures
 * thought_tree events for visualization.
 */
export function useThoughtEngine(agentId: string | null, apiBaseUrl?: string): UseThoughtEngineResult {
  const [currentTree, setCurrentTree] = useState<ThoughtTree | null>(null)
  const [history, setHistory] = useState<ThoughtTree[]>([])
  const [connected, setConnected] = useState(false)
  const [paused, setPaused] = useState(false)
  const eventSourceRef = useRef<EventSource | null>(null)

  const baseUrl = apiBaseUrl || `${window.location.protocol}//${window.location.host}`

  const connect = useCallback((agent: string) => {
    // Close existing connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }

    const url = `${baseUrl}/api/agents/${agent}/stream`
    console.log('[ThoughtEngine] Connecting to SSE:', url)
    const es = new EventSource(url)
    eventSourceRef.current = es

    es.onopen = () => {
      console.log('[ThoughtEngine] SSE connected')
      setConnected(true)
    }
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        setConnected(false)
      }
    }

    // Listen for the 'connected' named event
    es.addEventListener('connected', (event) => {
      console.log('[ThoughtEngine] connected event:', event.data)
      setConnected(true)
    })

    // Handle incremental updates (partial tree during evaluation)
    const handleUpdate = (event: MessageEvent) => {
      try {
        const parsed: SSEEvent = JSON.parse(event.data)
        const tree = (parsed.data ?? parsed) as ThoughtTree
        setCurrentTree(tree)
      } catch {
        // ignore parse errors
      }
    }

    // Handle final complete tree
    const handleComplete = (event: MessageEvent) => {
      try {
        const parsed: SSEEvent = JSON.parse(event.data)
        const tree = (parsed.data ?? parsed) as ThoughtTree
        setCurrentTree(tree)
        setHistory(prev => {
          // Deduplicate by ID
          const filtered = prev.filter(t => t.id !== tree.id)
          return [tree, ...filtered].slice(0, MAX_HISTORY)
        })
      } catch {
        // ignore parse errors
      }
    }

    es.addEventListener('thought_tree_update', handleUpdate)
    es.addEventListener('thought_tree', handleComplete)

    // Listen for pause/resume events
    es.addEventListener('paused', () => setPaused(true))
    es.addEventListener('resumed', () => setPaused(false))
  }, [baseUrl])

  // Fetch initial pause state when agent changes
  useEffect(() => {
    if (agentId) {
      connect(agentId)
      // Fetch current pause state from agent details
      fetch(`${baseUrl}/api/agents/${agentId}/details`)
        .then(r => r.ok ? r.json() : null)
        .then(data => { if (data) setPaused(!!data.paused) })
        .catch(() => {})
    } else {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
        eventSourceRef.current = null
      }
      setConnected(false)
      setPaused(false)
    }

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close()
        eventSourceRef.current = null
      }
    }
  }, [agentId, connect, baseUrl])

  return { currentTree, history, connected, paused }
}

export type { ThoughtTree, ThoughtNodeData, AxisScores }
