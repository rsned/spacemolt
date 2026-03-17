interface RadarChartProps {
  scores: {
    survival: number
    profit: number
    goal_progress: number
    risk: number
    efficiency: number
  }
  size?: number
}

const AXES = ['survival', 'profit', 'goal_progress', 'risk', 'efficiency'] as const
const LABELS = ['SRV', 'PRF', 'GOL', 'RSK', 'EFF']

export const RadarChart: React.FC<RadarChartProps> = ({ scores, size = 80 }) => {
  const center = size / 2
  const radius = size * 0.38
  const labelRadius = size * 0.48
  const n = AXES.length
  // Pentagon: start at top (angle = -90 degrees)
  const angleStep = (2 * Math.PI) / n
  const startAngle = -Math.PI / 2

  const getPoint = (index: number, r: number): [number, number] => {
    const angle = startAngle + index * angleStep
    return [center + r * Math.cos(angle), center + r * Math.sin(angle)]
  }

  const gridLevels = [0.25, 0.5, 0.75, 1.0]

  const gridPolygons = gridLevels.map((level) => {
    const points = AXES.map((_, i) => getPoint(i, radius * level))
    return points.map(([x, y]) => `${x},${y}`).join(' ')
  })

  const dataPoints = AXES.map((axis, i) => {
    const val = Math.min(100, Math.max(0, scores[axis] ?? 0)) / 100
    return getPoint(i, radius * val)
  })
  const dataPolygon = dataPoints.map(([x, y]) => `${x},${y}`).join(' ')

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="flex-shrink-0">
      {/* Grid lines from center */}
      {AXES.map((_, i) => {
        const [x, y] = getPoint(i, radius)
        return (
          <line
            key={`spoke-${i}`}
            x1={center}
            y1={center}
            x2={x}
            y2={y}
            stroke="#374151"
            strokeWidth={0.5}
          />
        )
      })}

      {/* Grid level polygons */}
      {gridPolygons.map((pts, li) => (
        <polygon
          key={`grid-${li}`}
          points={pts}
          fill="none"
          stroke="#374151"
          strokeWidth={0.5}
        />
      ))}

      {/* Data polygon */}
      <polygon
        points={dataPolygon}
        fill="rgba(59, 130, 246, 0.3)"
        stroke="#3b82f6"
        strokeWidth={1.5}
      />

      {/* Data dots */}
      {dataPoints.map(([x, y], i) => (
        <circle key={`dot-${i}`} cx={x} cy={y} r={2} fill="#3b82f6" />
      ))}

      {/* Axis labels */}
      {AXES.map((_, i) => {
        const [x, y] = getPoint(i, labelRadius)
        return (
          <text
            key={`label-${i}`}
            x={x}
            y={y}
            textAnchor="middle"
            dominantBaseline="middle"
            fontSize={size * 0.1}
            fill="#9ca3af"
          >
            {LABELS[i]}
          </text>
        )
      })}
    </svg>
  )
}
