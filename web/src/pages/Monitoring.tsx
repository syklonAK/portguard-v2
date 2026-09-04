import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import { Badge, Card, CardHeader, Empty, Spinner, StatusDot } from '../components/ui'

export default function Monitoring() {
  const mappings = useQuery({ queryKey: ['mappings'], queryFn: () => api.listMappings() })
  const health = useQuery({ queryKey: ['health'], queryFn: () => api.healthList(), refetchInterval: 10000 })

  if (mappings.isLoading || health.isLoading) {
    return (
      <div className="flex justify-center py-20"><Spinner className="h-8 w-8" /></div>
    )
  }

  const healthMap = new Map(health.data?.map((h) => [`${h.mapping_id}:${h.target_index}`, h]))
  const enabled = (mappings.data || []).filter((m) => m.enabled && m.targets.length > 0)

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-bold">Monitoring</h1>
        <p className="text-xs text-slate-500 dark:text-slate-400">
          Live backend health — TCP + HTTP checks run periodically (auto-refresh every 10s)
        </p>
      </div>

      {enabled.length === 0 ? (
        <Card>
          <Empty message="No enabled mappings with targets to monitor yet." />
        </Card>
      ) : (
        <div className="space-y-4">
          {enabled.map((m) => (
            <Card key={m.id}>
              <CardHeader
                title={m.name}
                desc={`${m.engine} · ${m.protocol} · listen ${m.listen_ip}:${m.listen_port}`}
                right={
                  <div className="flex items-center gap-1.5">
                    {m.server_names.length > 0 && <Badge color="blue">{m.server_names[0]}</Badge>}
                  </div>
                }
              />
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                    <th className="px-5 py-2 font-medium">Target</th>
                    <th className="px-3 py-2 font-medium">Status</th>
                    <th className="px-3 py-2 font-medium">Latency</th>
                    <th className="px-3 py-2 font-medium">Fails</th>
                    <th className="px-5 py-2 font-medium">Last check</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
                  {m.targets.map((t, i) => {
                    const h = healthMap.get(`${m.id}:${i}`)
                    const status = (h?.status || 'unknown') as 'up' | 'down' | 'unknown'
                    return (
                      <tr key={i} className="hover:bg-slate-50 dark:hover:bg-slate-800/40">
                        <td className="px-5 py-2.5 font-mono text-xs">{t.host}:{t.port}{t.backup ? ' (backup)' : ''}</td>
                        <td className="px-3 py-2.5">
                          <span className="inline-flex items-center gap-1.5">
                            <StatusDot status={status} />
                            <Badge color={status === 'up' ? 'green' : status === 'down' ? 'red' : 'slate'}>{status}</Badge>
                          </span>
                        </td>
                        <td className="px-3 py-2.5 font-mono text-xs">{h ? `${h.latency_ms.toFixed(1)} ms` : '—'}</td>
                        <td className="px-3 py-2.5 text-xs">{h?.fail_count ? <Badge color="red">{h.fail_count}</Badge> : '0'}</td>
                        <td className="px-5 py-2.5 text-xs text-slate-500">
                          {h?.last_check_at ? new Date(h.last_check_at).toLocaleTimeString() : '—'}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
