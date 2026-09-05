import { useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell, Check, FlaskConical, RefreshCw } from 'lucide-react'
import { api, type Alert } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'

const SEV_COLORS: Record<string, 'green' | 'amber' | 'red' | 'slate'> = {
  info: 'green',
  warning: 'amber',
  critical: 'red',
}

export default function Alerts() {
  const qc = useQueryClient()
  const { push } = useToast()

  const alerts = useQuery({
    queryKey: ['alerts'],
    queryFn: () => api.listAlerts(200),
    refetchInterval: 15000,
  })

  // live refresh via SSE bridge
  useEffect(() => {
    const h = () => qc.invalidateQueries({ queryKey: ['alerts'] })
    window.addEventListener('pg-sse-alert', h)
    return () => window.removeEventListener('pg-sse-alert', h)
  }, [qc])

  const ack = useMutation({
    mutationFn: (id: number) => api.ackAlert(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['alerts'] }),
    onError: (e: any) => push('error', e.message),
  })

  const test = useMutation({
    mutationFn: () => api.testAlert(),
    onSuccess: () => push('success', 'Test alert dispatched — check your channels and this list.'),
    onError: (e: any) => push('error', e.message),
  })

  const data = alerts.data
  const list: Alert[] = data?.alerts ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-lg font-bold">
            Alerts
            {(data?.unacknowledged ?? 0) > 0 && (
              <span className="rounded-full bg-red-500 px-2 py-0.5 text-2xs font-bold text-white">
                {data?.unacknowledged}
              </span>
            )}
          </h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Node offline, backend down, certificate expiry and resource thresholds — with dedup + cooldown.
            Channels (Telegram / webhook) are configured in Settings.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => alerts.refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button variant="secondary" onClick={() => test.mutate()} disabled={test.isPending}>
            <FlaskConical className="h-4 w-4" /> Test channels
          </Button>
        </div>
      </div>

      {!data?.enabled && data && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 p-3.5 text-xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
          Alerting is disabled — enable it in Settings → Alerting.
        </div>
      )}

      <Card>
        <CardHeader title="Alert history" desc={`${list.length} recent alerts`} />
        {alerts.isLoading ? (
          <div className="flex justify-center py-14"><Spinner /></div>
        ) : !list.length ? (
          <Empty message="No alerts yet — everything is quiet." />
        ) : (
          <div className="divide-y divide-slate-100 dark:divide-slate-800/70">
            {list.map((a) => (
              <div key={a.id} className={`flex items-start gap-3 px-5 py-3 ${a.acknowledged ? 'opacity-50' : ''}`}>
                <Badge color={SEV_COLORS[a.severity] || 'slate'}>
                  <Bell className="h-3 w-3" /> {a.severity}
                </Badge>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-medium">{a.title}</span>
                    <Badge color="slate">{a.category}</Badge>
                  </div>
                  <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{a.detail}</p>
                  <p className="mt-0.5 text-2xs text-slate-400">
                    {new Date(a.created_at).toLocaleString()}
                    {a.target && <> · target: <span className="font-mono">{a.target}</span></>}
                  </p>
                </div>
                {!a.acknowledged && (
                  <Button variant="ghost" size="sm" title="Acknowledge" onClick={() => ack.mutate(a.id)}>
                    <Check className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  )
}
