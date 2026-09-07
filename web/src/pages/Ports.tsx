import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { RefreshCw, ArrowLeftRight } from 'lucide-react'
import { api, type PortEntry } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Input, Select, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

function classColor(cls: string): 'green' | 'purple' | 'blue' | 'amber' | 'red' | 'slate' {
  switch (cls) {
    case 'web-server': return 'green'
    case 'load-balancer': return 'purple'
    case 'ssh': return 'blue'
    case 'xray-proxy': case 'proxy': case 'pasarguard-node': return 'amber'
    case 'database': return 'red'
    default: return 'slate'
  }
}

export default function Ports() {
  const qc = useQueryClient()
  const { push } = useToast()
  const [params] = useSearchParams()
  const [filter, setFilter] = useState<'all' | 'unmanaged' | 'managed'>('all')
  const [search, setSearch] = useState('')

  const ports = useQuery({ queryKey: ['ports'], queryFn: () => api.listPorts(), refetchInterval: 15000 })
  const scan = useMutation({
    mutationFn: () => api.scan(),
    onSuccess: (res) => {
      push('success', `Scan complete — ${res.count} listening sockets found`)
      qc.invalidateQueries({ queryKey: ['ports'] })
      qc.invalidateQueries({ queryKey: ['system'] })
    },
    onError: (e: any) => push('error', `Scan failed: ${e.message}`),
  })

  const rows = useMemo(() => {
    let list = ports.data?.ports || []
    if (filter === 'unmanaged') list = list.filter((p) => !p.managed && !p.self)
    if (filter === 'managed') list = list.filter((p) => p.managed || p.self)
    if (search) {
      const q = search.toLowerCase()
      list = list.filter(
        (p) => String(p.port).includes(q) || p.process.toLowerCase().includes(q) || p.classification.includes(q),
      )
    }
    return list
  }, [ports.data, filter, search])

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Ports</h1>
          <p className="text-xs text-muted-foreground">
            Listening sockets detected on this server
            {ports.data?.last_scan_at ? ` · last scan ${new Date(ports.data.last_scan_at).toLocaleString()}` : ''}
          </p>
        </div>
        <Button onClick={() => scan.mutate()} disabled={scan.isPending}>
          <RefreshCw className={`h-4 w-4 ${scan.isPending ? 'animate-spin' : ''}`} />
          Scan now
        </Button>
      </div>

      <Card>
        <CardHeader
          title="Listening Ports"
          desc="Managed = covered by a mapping or the proxy engines themselves"
          right={
            <div className="flex gap-2">
              <Select value={filter} onChange={(e) => setFilter(e.target.value as any)} className="w-36">
                <option value="all">All ports</option>
                <option value="unmanaged">Unmanaged</option>
                <option value="managed">Managed</option>
              </Select>
              <Input placeholder="Search…" value={search} onChange={(e) => setSearch(e.target.value)} className="w-44" />
            </div>
          }
        />
        {ports.isLoading ? (
          <div className="flex justify-center py-16"><Spinner /></div>
        ) : rows.length === 0 ? (
          <Empty message="No ports match. Run a scan first." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">Port</th>
                  <th className="px-3 py-2.5 font-medium">Proto</th>
                  <th className="px-3 py-2.5 font-medium">Listen IP</th>
                  <th className="px-3 py-2.5 font-medium">Process</th>
                  <th className="px-3 py-2.5 font-medium">PID</th>
                  <th className="px-3 py-2.5 font-medium">User</th>
                  <th className="px-3 py-2.5 font-medium">Class</th>
                  <th className="px-5 py-2.5 font-medium">Status</th>
                  <th className="px-5 py-2.5"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {rows.map((p: PortEntry, i) => (
                  <tr key={`${p.proto}-${p.port}-${p.listen_ip}-${i}`} className="hover:bg-muted/60">
                    <td className="px-5 py-2.5 font-mono font-semibold">{p.port}</td>
                    <td className="px-3 py-2.5">
                      <Badge color={p.proto === 'tcp' ? 'blue' : 'purple'}>{p.proto}</Badge>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-xs text-muted-foreground">{p.listen_ip}</td>
                    <td className="px-3 py-2.5">{p.process || <span className="text-muted-foreground">—</span>}</td>
                    <td className="px-3 py-2.5 text-xs text-muted-foreground">{p.pid || '—'}</td>
                    <td className="px-3 py-2.5 text-xs text-muted-foreground">{p.user || '—'}</td>
                    <td className="px-3 py-2.5">
                      <Badge color={classColor(p.classification)}>{p.classification}</Badge>
                    </td>
                    <td className="px-5 py-2.5">
                      {p.self ? (
                        <Badge color="blue">portguard</Badge>
                      ) : p.managed ? (
                        <Badge color="green">managed</Badge>
                      ) : (
                        <Badge color="amber">unmanaged</Badge>
                      )}
                    </td>
                    <td className="px-5 py-2.5 text-right">
                      {!p.managed && !p.self && p.proto === 'tcp' && (
                        <Link to={`/mappings?new=1&port=${p.port}`} className="inline-flex" title="Create mapping for this port">
                          <Button variant="ghost" size="sm">
                            <ArrowLeftRight className="h-3.5 w-3.5" />
                            Map
                          </Button>
                        </Link>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  )
}
