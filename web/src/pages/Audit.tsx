import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Spinner } from '../components/ui'

const actionColor: Record<string, 'green' | 'red' | 'blue' | 'amber' | 'purple' | 'slate'> = {
  'apply': 'green',
  'login': 'blue',
  'mapping.create': 'purple',
  'mapping.update': 'amber',
  'mapping.delete': 'red',
  'cert.create': 'blue',
  'cert.delete': 'red',
  'cert.selfsigned': 'blue',
  'settings.update': 'slate',
  'setup': 'purple',
  'password': 'slate',
}

export default function Audit() {
  const qc = useQueryClient()
  const audit = useQuery({ queryKey: ['audit'], queryFn: () => api.audit(), refetchInterval: 20000 })

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold">Audit Log</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">Who did what — every state-changing action is recorded</p>
        </div>
        <Button variant="secondary" onClick={() => qc.invalidateQueries({ queryKey: ['audit'] })}>
          <RefreshCw className="h-4 w-4" /> Refresh
        </Button>
      </div>

      <Card>
        <CardHeader title="Recent activity" desc="Latest 300 entries" />
        {audit.isLoading ? (
          <div className="flex justify-center py-16"><Spinner /></div>
        ) : !audit.data?.length ? (
          <Empty message="Nothing logged yet." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                  <th className="px-5 py-2.5 font-medium">Time</th>
                  <th className="px-3 py-2.5 font-medium">Actor</th>
                  <th className="px-3 py-2.5 font-medium">Action</th>
                  <th className="px-3 py-2.5 font-medium">Detail</th>
                  <th className="px-5 py-2.5 font-medium">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
                {audit.data.map((l) => (
                  <tr key={l.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/40">
                    <td className="px-5 py-2.5 whitespace-nowrap font-mono text-xs text-slate-500">
                      {new Date(l.created_at).toLocaleString()}
                    </td>
                    <td className="px-3 py-2.5 font-medium">{l.actor}</td>
                    <td className="px-3 py-2.5"><Badge color={actionColor[l.action] || 'slate'}>{l.action}</Badge></td>
                    <td className="px-3 py-2.5 text-xs text-slate-500">{l.detail}</td>
                    <td className="px-5 py-2.5"><Badge color={l.status === 'ok' ? 'green' : 'red'}>{l.status}</Badge></td>
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
