import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, ShieldAlert, ShieldCheck, TrendingUp } from 'lucide-react'
import { api, type ConnEntry, type TopTalker } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Input, Select, Spinner, StatusDot } from '../components/ui'

type Filter = 'all' | 'managed' | 'unmanaged'

export default function Connections() {
  const qc = useQueryClient()
  const [filter, setFilter] = useState<Filter>('all')
  const [search, setSearch] = useState('')
  const [live, setLive] = useState(true)

  const conns = useQuery({
    queryKey: ['connections'],
    queryFn: () => api.connections(),
    refetchInterval: live ? 4000 : false,
  })

  // instant refresh on SSE conns events (bridged in main.tsx)
  useEffect(() => {
    if (!live) return
    const handler = () => qc.invalidateQueries({ queryKey: ['connections'] })
    window.addEventListener('pg-sse-conns', handler)
    return () => window.removeEventListener('pg-sse-conns', handler)
  }, [live, qc])

  const data = conns.data

  const filtered: ConnEntry[] = useMemo(() => {
    let list = data?.connections ?? []
    if (filter === 'managed') list = list.filter((c) => c.managed)
    if (filter === 'unmanaged') list = list.filter((c) => !c.managed)
    if (search.trim()) {
      const s = search.trim().toLowerCase()
      list = list.filter((c) =>
        c.src_ip.toLowerCase().includes(s) ||
        String(c.dst_port).includes(s) ||
        c.process.toLowerCase().includes(s)
      )
    }
    return list
  }, [data, filter, search])

  const talkers: TopTalker[] = data?.top_talkers ?? []
  const unmanagedCount = (data?.connections ?? []).filter((c) => !c.managed && !c.self).length
  const selfCount = (data?.connections ?? []).filter((c) => c.self).length

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-lg font-bold">
            Live Connections
            <span className={`inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-2xs font-medium ${live ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300' : 'bg-mutedslate text-muted-foreground'}`}>
              <StatusDot status={live ? 'up' : 'unknown'} /> {live ? 'live' : 'paused'}
            </span>
          </h1>
          <p className="text-xs text-muted-foreground">
            Who is connected to this server right now — source IP, target port and the serving process.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            className="w-44"
            placeholder="IP / port / process"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Select className="w-44" value={filter} onChange={(e) => setFilter(e.target.value as Filter)}>
            <option value="all">All connections</option>
            <option value="managed">Managed ports only</option>
            <option value="unmanaged">Unmanaged only</option>
          </Select>
          <Button variant={live ? 'secondary' : 'primary'} onClick={() => setLive((v) => !v)}>
            <Activity className="h-4 w-4" /> {live ? 'Pause' : 'Resume live'}
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-4">
        <Card className="lg:col-span-3">
          <CardHeader
            title="Inbound connections"
            desc={`${filtered.length} shown${data ? ` of ${data.total} live` : ''} — snapshot refreshed every few seconds`}
            right={
              <div className="flex gap-1.5">
                <Badge color="amber">{unmanagedCount} unmanaged</Badge>
                <Badge color="blue">{selfCount} to panel</Badge>
              </div>
            }
          />
          {conns.isLoading ? (
            <div className="flex justify-center py-14"><Spinner /></div>
          ) : filtered.length === 0 ? (
            <Empty message="No matching live connections. Incoming ESTAB sessions appear here in real time." />
          ) : (
            <div className="max-h-[540px] overflow-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-card">
                  <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                    <th className="px-5 py-2.5 font-medium">Source IP : port</th>
                    <th className="px-3 py-2.5 font-medium">Target port</th>
                    <th className="px-3 py-2.5 font-medium">Process</th>
                    <th className="px-3 py-2.5 font-medium">Since</th>
                    <th className="px-5 py-2.5 font-medium">Class</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {filtered.map((c, i) => (
                    <tr key={`${c.src_ip}:${c.src_port}-${c.dst_ip}:${c.dst_port}-${i}`} className="hover:bg-muted/60">
                      <td className="px-5 py-2.5 font-mono text-xs">
                        {c.src_ip}
                        <span className="text-muted-foreground">:{c.src_port}</span>
                      </td>
                      <td className="px-3 py-2.5 font-mono text-xs">
                        {c.dst_ip}:{c.dst_port}
                      </td>
                      <td className="px-3 py-2.5 text-xs">
                        {c.process ? <span className="font-mono">{c.process}</span> : <span className="text-muted-foreground">—</span>}
                      </td>
                      <td className="px-3 py-2.5 text-2xs text-muted-foreground">
                        {ago(c.first_seen)}
                      </td>
                      <td className="px-5 py-2.5">
                        {c.self ? (
                          <Badge color="blue">panel session</Badge>
                        ) : c.managed ? (
                          <Badge color="green"><ShieldCheck className="h-3 w-3" /> managed</Badge>
                        ) : (
                          <Badge color="red"><ShieldAlert className="h-3 w-3" /> unmanaged</Badge>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card>
          <CardHeader title="Top talkers" desc="Noisiest external IPs right now" />
          <div className="space-y-2 p-4">
            {talkers.length === 0 ? (
              <p className="py-8 text-center text-xs text-muted-foreground">No external sources connected.</p>
            ) : (
              talkers.map((t) => (
                <div key={t.src_ip} className="rounded-lg border border-border p-2.5">
                  <div className="flex items-center justify-between">
                    <span className="font-mono text-xs font-medium">{t.src_ip}</span>
                    <span className="flex items-center gap-1 text-2xs font-semibold text-primary">
                      <TrendingUp className="h-3 w-3" /> {t.conns}
                    </span>
                  </div>
                  {t.targets && <p className="mt-1 truncate font-mono text-2xs text-muted-foreground" title={t.targets}>{t.targets}</p>}
                  <p className="mt-0.5 text-2xs text-muted-foreground">connected since {ago(t.first_seen)}</p>
                </div>
              ))
            )}
          </div>
          <div className="border-t border-border px-4 py-3">
            <p className="text-2xs leading-relaxed text-muted-foreground">
              <b>Unmanaged</b> = traffic hitting a port that is not covered by any PortGuard mapping — investigate, then map or firewall it.
              Connections marked <b>panel session</b> are admins using this dashboard.
            </p>
          </div>
        </Card>
      </div>
    </div>
  )
}

function ago(ts: number): string {
  if (!ts) return '—'
  const s = Math.max(0, Math.floor(Date.now() / 1000 - ts))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ${m % 60}m`
  return `${Math.floor(h / 24)}d ${h % 24}h`
}
