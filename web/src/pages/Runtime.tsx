import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { api, fmtBytes } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

export default function Runtime() {
  const qc = useQueryClient()
  const { push } = useToast()
  const [filter, setFilter] = useState('')
  const runtime = useQuery({ queryKey: ['runtime'], queryFn: () => api.runtime(), refetchInterval: 10000 })

  const setState = useMutation({
    mutationFn: (v: { backend: string; server: string; state: 'ready' | 'drain' | 'maint' }) =>
      api.runtimeSetServerState(v.backend, v.server, v.state),
    onSuccess: () => {
      push('success', 'Server state changed')
      qc.invalidateQueries({ queryKey: ['runtime'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  if (runtime.isLoading) {
    return <div className="flex justify-center py-16"><Spinner /></div>
  }

  const rt = runtime.data
  if (!rt?.available) {
    return (
      <div className="space-y-4">
        <Header />
        <Card>
          <Empty message={`HAProxy Runtime API is not available (${rt?.error || 'socket not found'}). Add "stats socket" to the haproxy global section — see Settings for the socket path.`} />
        </Card>
      </div>
    )
  }

  const summary = rt.summary || { frontends: 0, backends: 0, servers: 0, up: 0, down: 0, maint: 0, sessions: 0, bytes_in: 0, bytes_out: 0 }
  const servers = (rt.servers || []).filter((r) => {
    if (!filter) return true
    const q = filter.toLowerCase()
    return r.pxname?.toLowerCase().includes(q) || r.svname?.toLowerCase().includes(q)
  })

  const statusColor = (status: string): 'green' | 'red' | 'amber' | 'slate' => {
    if (status.startsWith('UP') || status === 'OPEN' || status === 'no check') return 'green'
    if (status.includes('DOWN')) return 'red'
    if (status.includes('MAINT')) return 'amber'
    return 'slate'
  }

  return (
    <div className="space-y-4">
      <Header
        right={
          <Button variant="secondary" size="sm" onClick={() => runtime.refetch()}>
            <RefreshCw className="h-3.5 w-3.5" /> Refresh
          </Button>
        }
      />

      {/* Summary */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
        {[
          ['Frontends', summary.frontends], ['Backends', summary.backends], ['Servers', summary.servers],
          ['UP', summary.up], ['DOWN', summary.down], ['MAINT', summary.maint],
        ].map(([label, value]) => (
          <Card key={label as string} className="p-3 text-center">
            <div className="text-xl font-bold">{value as number}</div>
            <div className="text-2xs text-slate-400">{label as string}</div>
          </Card>
        ))}
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader title="show info" desc={`Runtime socket: ${rt.socket}`} />
          <div className="max-h-72 overflow-y-auto p-5 pt-3">
            <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
              {Object.entries(rt.info || {}).map(([k, v]) => (
                <div key={k} className="contents">
                  <span className="text-slate-400">{k}</span>
                  <span className="font-mono text-slate-600 dark:text-slate-300">{v}</span>
                </div>
              ))}
            </div>
          </div>
        </Card>

        <Card>
          <CardHeader title="Traffic" desc="Cumulative counters from show stat" />
          <div className="grid grid-cols-2 gap-4 p-5">
            <div className="rounded-lg bg-slate-50 p-3 dark:bg-slate-800">
              <div className="text-lg font-bold">{fmtBytes(summary.bytes_in)}</div>
              <div className="text-2xs text-slate-400">bytes in</div>
            </div>
            <div className="rounded-lg bg-slate-50 p-3 dark:bg-slate-800">
              <div className="text-lg font-bold">{fmtBytes(summary.bytes_out)}</div>
              <div className="text-2xs text-slate-400">bytes out</div>
            </div>
            <div className="rounded-lg bg-slate-50 p-3 dark:bg-slate-800">
              <div className="text-lg font-bold">{summary.sessions}</div>
              <div className="text-2xs text-slate-400">current sessions</div>
            </div>
          </div>
        </Card>
      </div>

      {/* Servers */}
      <Card>
        <CardHeader
          title="Backend servers"
          desc="Live status — put a server into maintenance or bring it back without a reload"
          right={
            <input
              className="h-8 w-44 rounded-lg border border-slate-300 bg-white px-2.5 text-xs dark:border-slate-600 dark:bg-slate-800"
              placeholder="filter…"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          }
        />
        {servers.length === 0 ? (
          <Empty message="No HAProxy mappings are applied yet." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                  <th className="px-5 py-2.5 font-medium">Backend</th>
                  <th className="px-3 py-2.5 font-medium">Server</th>
                  <th className="px-3 py-2.5 font-medium">Status</th>
                  <th className="px-3 py-2.5 font-medium">Sessions</th>
                  <th className="px-3 py-2.5 font-medium">In / Out</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
                {servers.map((r, i) => (
                  <tr key={`${r.pxname}-${r.svname}-${i}`} className="hover:bg-slate-50 dark:hover:bg-slate-800/40">
                    <td className="px-5 py-2.5 font-mono text-xs">{r.pxname}</td>
                    <td className="px-3 py-2.5 font-mono text-xs">{r.svname}</td>
                    <td className="px-3 py-2.5"><Badge color={statusColor(r.status || '')}>{r.status || '—'}</Badge></td>
                    <td className="px-3 py-2.5 font-mono text-xs">{r.scur || '0'}</td>
                    <td className="px-3 py-2.5 font-mono text-xs">{fmtBytes(Number(r.bin || 0))} / {fmtBytes(Number(r.bout || 0))}</td>
                    <td className="px-5 py-2.5 text-right">
                      <div className="flex justify-end gap-1">
                        <Button size="sm" variant="ghost" disabled={setState.isPending}
                          onClick={() => setState.mutate({ backend: r.pxname!, server: r.svname!, state: 'ready' })}>
                          ready
                        </Button>
                        <Button size="sm" variant="ghost" disabled={setState.isPending}
                          onClick={() => setState.mutate({ backend: r.pxname!, server: r.svname!, state: 'drain' })}>
                          drain
                        </Button>
                        <Button size="sm" variant="ghost" className="text-amber-600" disabled={setState.isPending}
                          onClick={() => setState.mutate({ backend: r.pxname!, server: r.svname!, state: 'maint' })}>
                          maint
                        </Button>
                      </div>
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

function Header({ right }: { right?: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 className="text-lg font-bold">HAProxy Runtime</h1>
        <p className="text-xs text-slate-500 dark:text-slate-400">
          Live statistics and server maintenance via the HAProxy stats socket
        </p>
      </div>
      {right}
    </div>
  )
}
