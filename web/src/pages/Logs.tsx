import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw, Terminal, Download } from 'lucide-react'
import { api, type ServerNode } from '../api'
import { Badge, Button, Card, CardHeader, Select, Spinner, Toggle } from '../components/ui'

const SOURCES = [
  { value: 'agent', label: 'PortGuard agent' },
  { value: 'panel', label: 'PortGuard panel' },
  { value: 'nginx', label: 'nginx (error.log)' },
  { value: 'haproxy', label: 'HAProxy' },
  { value: 'xray', label: 'Xray' },
  { value: 'hedioum', label: 'Hedioum tunnel' },
  { value: 'bridge', label: 'Tunnel bridge' },
  { value: 'syslog', label: 'Syslog' },
]

export default function Logs() {
  const [nodeId, setNodeId] = useState<number | null>(null)
  const [source, setSource] = useState('agent')
  const [lines, setLines] = useState(200)
  const [live, setLive] = useState(true)

  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.listNodes() })
  const effectiveNode = nodeId ?? nodes.data?.[0]?.id ?? null

  const logs = useQuery({
    queryKey: ['node-logs', effectiveNode, source, lines],
    queryFn: () => api.nodeLogs(effectiveNode!, source, lines),
    enabled: effectiveNode != null,
    refetchInterval: live ? 5000 : false,
  })

  const content = logs.data?.lines ?? ''
  const errCount = (content.match(/\b(error|crit|emerg|alert)\b/gi) || []).length

  const download = () => {
    const blob = new Blob([content], { type: 'text/plain' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `${source}-node${effectiveNode}.log`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Logs</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Allowlisted sources only (agent, nginx, haproxy, xray, hedioum, bridge, syslog) — bounded tails, never unrestricted file access.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Select className="w-44" value={effectiveNode ?? 0} onChange={(e) => setNodeId(parseInt(e.target.value, 10))}>
            {(nodes.data ?? []).map((n: ServerNode) => (
              <option key={n.id} value={n.id}>{n.name}{n.status === 'offline' ? ' (offline)' : ''}</option>
            ))}
          </Select>
          <Select className="w-44" value={source} onChange={(e) => setSource(e.target.value)}>
            {SOURCES.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
          </Select>
          <Select className="w-24" value={lines} onChange={(e) => setLines(parseInt(e.target.value, 10))}>
            <option value={100}>100</option>
            <option value={200}>200</option>
            <option value={500}>500</option>
          </Select>
          <Toggle checked={live} onChange={setLive} />
          <Button variant="secondary" onClick={() => logs.refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button variant="secondary" onClick={download} disabled={!content}>
            <Download className="h-4 w-4" />
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader
          title={`${SOURCES.find((s) => s.value === source)?.label} — ${nodes.data?.find((n) => n.id === effectiveNode)?.name ?? '…'}`}
          desc={`${content ? content.split('\n').length : 0} lines${errCount ? ` · ${errCount} error-ish` : ''}`}
          right={live ? <Badge color="green">live</Badge> : <Badge color="slate">paused</Badge>}
        />
        {logs.isLoading ? (
          <div className="flex justify-center py-14"><Spinner /></div>
        ) : logs.error ? (
          <p className="p-6 text-center text-xs text-red-500">{(logs.error as any).message}</p>
        ) : (
          <pre className="max-h-[600px] overflow-auto rounded-b-xl bg-slate-950 p-4 font-mono text-2xs leading-relaxed">
            {content.split('\n').map((line, i) => (
              <div key={i} className={
                /\bemerg|alert|crit\b/i.test(line) ? 'text-red-400'
                : /\berror\b/i.test(line) ? 'text-amber-400'
                : /\bwarn\b/i.test(line) ? 'text-yellow-300'
                : 'text-slate-300'
              }>{line || ' '}</div>
            ))}
          </pre>
        )}
      </Card>
    </div>
  )
}
