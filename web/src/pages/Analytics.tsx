import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fmtBps, fmtBytes } from '../lib/format'
import { Activity, ArrowDown, ArrowUp, Gauge, Zap } from 'lucide-react'
import { api, type MetricsData, type MetricPoint, type ServerNode } from '../api'
import { Badge, Card, CardHeader, Select, Spinner } from '../components/ui'

type Range = '5m' | '1h' | '24h' | '7d'
const RANGES: { value: Range; label: string }[] = [
  { value: '5m', label: 'Last 5 minutes' },
  { value: '1h', label: 'Last hour' },
  { value: '24h', label: 'Last 24 hours' },
  { value: '7d', label: 'Last 7 days' },
]

// Sparkline renders an SVG area chart from numeric points.
function Sparkline({ points, color, height = 90 }: { points: number[]; color: string; height?: number }) {
  const W = 560
  if (points.length < 2) {
    return <div className="flex items-center justify-center text-2xs text-slate-400" style={{ height }}>not enough samples yet</div>
  }
  const max = Math.max(...points, 1)
  const step = W / (points.length - 1)
  const path = points.map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)},${(height - (v / max) * (height - 6) - 3).toFixed(1)}`).join(' ')
  const area = `${path} L${W},${height} L0,${height} Z`
  return (
    <svg viewBox={`0 0 ${W} ${height}`} className="w-full" style={{ height }}>
      <path d={area} fill={color} opacity={0.15} />
      <path d={path} fill="none" stroke={color} strokeWidth={1.6} />
    </svg>
  )
}

export default function Analytics() {
  const [nodeId, setNodeId] = useState<number>(0) // 0 = this panel
  const [range, setRange] = useState<Range>('1h')

  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.listNodes() })
  const metrics = useQuery({
    queryKey: ['metrics', nodeId, range],
    queryFn: () => api.metrics(nodeId, range),
    refetchInterval: 15000,
  })

  const d: MetricsData | undefined = metrics.data
  const pts: MetricPoint[] = d?.points ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Traffic Analytics</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Per-node bandwidth, connections and resource history — sampled every 30s, 7-day retention.
          </p>
        </div>
        <div className="flex gap-2">
          <Select className="w-48" value={nodeId} onChange={(e) => setNodeId(parseInt(e.target.value, 10))}>
            <option value={0}>This panel (master)</option>
            {(nodes.data ?? []).map((n: ServerNode) => (
              <option key={n.id} value={n.id}>{n.name}</option>
            ))}
          </Select>
          <Select className="w-40" value={range} onChange={(e) => setRange(e.target.value as Range)}>
            {RANGES.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
          </Select>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Stat icon={<Activity className="h-4 w-4" />} label="Active connections" value={String(d?.active_conns ?? '—')} />
        <Stat icon={<ArrowDown className="h-4 w-4" />} label="RX total" value={d ? fmtBytes(d.rx_total) : '—'} />
        <Stat icon={<ArrowUp className="h-4 w-4" />} label="TX total" value={d ? fmtBytes(d.tx_total) : '—'} />
        <Stat icon={<Zap className="h-4 w-4" />} label="RX peak" value={d ? fmtBps(d.rx_peak) : '—'} accent="indigo" />
        <Stat icon={<Gauge className="h-4 w-4" />} label="TX peak" value={d ? fmtBps(d.tx_peak) : '—'} accent="indigo" />
      </div>

      {metrics.isLoading ? (
        <div className="flex justify-center py-20"><Spinner /></div>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader title="Download (RX)" desc={`peak ${d ? fmtBps(d.rx_peak) : '—'} · current ${d && pts.length ? fmtBps(pts[pts.length - 1].rx_bps) : '—'}`} />
            <div className="p-4">
              <Sparkline points={pts.map((p) => p.rx_bps)} color="#6366f1" />
            </div>
          </Card>
          <Card>
            <CardHeader title="Upload (TX)" desc={`peak ${d ? fmtBps(d.tx_peak) : '—'} · current ${d && pts.length ? fmtBps(pts[pts.length - 1].tx_bps) : '—'}`} />
            <div className="p-4">
              <Sparkline points={pts.map((p) => p.tx_bps)} color="#10b981" />
            </div>
          </Card>
          <Card>
            <CardHeader title="Connections" desc={`${pts.length} samples`} right={<Badge color="slate">{d?.active_conns ?? 0} now</Badge>} />
            <div className="p-4">
              <Sparkline points={pts.map((p) => p.conns)} color="#0ea5e9" />
            </div>
          </Card>
          <Card>
            <CardHeader title="CPU / RAM / Disk %" desc="resource history" />
            <div className="space-y-1 p-4">
              <Sparkline points={pts.map((p) => p.cpu_percent)} color="#ef4444" height={40} />
              <Sparkline points={pts.map((p) => p.mem_percent)} color="#f59e0b" height={40} />
              <Sparkline points={pts.map((p) => p.disk_percent)} color="#64748b" height={40} />
            </div>
          </Card>
        </div>
      )}
    </div>
  )
}

function Stat({ icon, label, value, accent }: { icon: React.ReactNode; label: string; value: string; accent?: 'indigo' }) {
  return (
    <div className="rounded-xl border border-slate-200 p-3.5 dark:border-slate-700">
      <div className="flex items-center gap-1.5 text-2xs font-medium uppercase tracking-wide text-slate-400">
        {icon} {label}
      </div>
      <div className={`mt-1 text-lg font-bold ${accent === 'indigo' ? 'text-indigo-600 dark:text-indigo-300' : ''}`}>{value}</div>
    </div>
  )
}
