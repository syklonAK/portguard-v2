import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Play, Square, RotateCw, RefreshCw, Power, PowerOff } from 'lucide-react'
import { api } from '../api'
import { Badge, Button, Card, CardHeader, Spinner, StatusDot } from '../components/ui'
import { useToast } from '../components/toast'

const ACTIONS = [
  { id: 'start', label: 'Start', icon: Play, variant: 'success' as const },
  { id: 'reload', label: 'Reload', icon: RefreshCw, variant: 'secondary' as const },
  { id: 'restart', label: 'Restart', icon: RotateCw, variant: 'secondary' as const },
  { id: 'stop', label: 'Stop', icon: Square, variant: 'danger' as const },
  { id: 'enable', label: 'Enable', icon: Power, variant: 'ghost' as const },
  { id: 'disable', label: 'Disable', icon: PowerOff, variant: 'ghost' as const },
]

export default function Services() {
  const qc = useQueryClient()
  const { push } = useToast()
  const services = useQuery({ queryKey: ['services'], queryFn: () => api.services(), refetchInterval: 8000 })

  const act = useMutation({
    mutationFn: ({ engine, action }: { engine: string; action: string }) => api.serviceAction(engine, action),
    onSuccess: (res) => {
      push('success', res.output ? res.output : 'Done')
      qc.invalidateQueries({ queryKey: ['services'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  const stateBadge = (active?: string, enabledState?: string) => (
    <>
      <Badge color={active === 'active' ? 'green' : active === 'failed' ? 'red' : 'slate'}>
        {active || 'unknown'}
      </Badge>
      <Badge color={enabledState === 'enabled' ? 'blue' : 'slate'}>{enabledState || '—'}</Badge>
    </>
  )

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-bold">Services</h1>
        <p className="text-xs text-slate-500 dark:text-slate-400">
          Start / stop / reload the proxy engines. Reload validates the generated config first — a broken config never
          reaches the service.
        </p>
      </div>

      {services.isLoading ? (
        <div className="flex justify-center py-16"><Spinner /></div>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {['nginx', 'haproxy'].map((engine) => {
            const s = services.data?.[engine]
            return (
              <Card key={engine}>
                <CardHeader
                  title={engine === 'nginx' ? 'Nginx' : 'HAProxy'}
                  desc={s?.description || (s?.binary_installed ? 'systemd unit' : 'binary not installed')}
                  right={
                    <div className="flex items-center gap-2">
                      <StatusDot status={s?.active === 'active' ? 'up' : s?.active === 'failed' ? 'down' : 'unknown'} />
                      {stateBadge(s?.active, s?.enabled_state)}
                    </div>
                  }
                />
                <div className="space-y-3 p-5">
                  {!s?.binary_installed && (
                    <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
                      The {engine} binary was not found on this server — install it or fix the path in Settings.
                    </p>
                  )}
                  <div className="flex flex-wrap gap-2">
                    {ACTIONS.map(({ id, label, icon: Icon, variant }) => (
                      <Button
                        key={id}
                        size="sm"
                        variant={variant}
                        disabled={act.isPending || !s?.binary_installed}
                        onClick={() => act.mutate({ engine, action: id })}
                      >
                        <Icon className="h-3.5 w-3.5" />
                        {label}
                      </Button>
                    ))}
                  </div>
                  <div className="grid grid-cols-2 gap-x-4 gap-y-1 border-t border-slate-100 pt-3 text-2xs text-slate-500 dark:border-slate-800">
                    <span>Unit</span><span className="font-mono">{s?.unit || engine}</span>
                    <span>Sub state</span><span>{s?.sub || '—'}</span>
                    <span>Main PID</span><span className="font-mono">{s?.pid || '—'}</span>
                  </div>
                  {s?.error && <p className="text-2xs text-red-500">{s.error}</p>}
                </div>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}
