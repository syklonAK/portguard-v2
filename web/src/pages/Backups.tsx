import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { History, RotateCcw, Trash2, GitCompare, FileText } from 'lucide-react'
import { api, fmtBytes, type BackupDiff as BackupDiffT } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Modal, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

function DiffView({ diffs }: { diffs: BackupDiffT[] }) {
  if (!diffs.length) return <Empty message="No files in this backup." />
  const anySame = diffs.every((d) => d.same)
  return (
    <div className="space-y-4">
      {diffs.map((d, i) => (
        <div key={i}>
          <div className="mb-1.5 flex items-center gap-2">
            <Badge color={d.engine === 'nginx' ? 'green' : 'purple'}>{d.engine || 'unknown'}</Badge>
            <span className="font-mono text-xs text-muted-foreground">{d.live_path}</span>
            {d.same ? <Badge color="slate">identical to live</Badge> : <Badge color="amber">differs from live</Badge>}
          </div>
          {!d.same && (
            <pre className="max-h-64 overflow-auto rounded-lg bg-popover p-3 font-mono text-2xs leading-relaxed">
              {d.diff.split('\n').map((line, j) => (
                <div key={j} className={
                  line.startsWith('+') ? 'text-emerald-400' :
                  line.startsWith('-') ? 'text-red-400' :
                  line.startsWith('@@') ? 'text-sky-400' : 'text-muted-foreground'
                }>{line}</div>
              ))}
            </pre>
          )}
        </div>
      ))}
      {anySame && <p className="text-xs text-muted-foreground">Every file in this backup matches the current live configuration.</p>}
    </div>
  )
}

export default function Backups() {
  const qc = useQueryClient()
  const { push } = useToast()
  const backups = useQuery({ queryKey: ['backups'], queryFn: () => api.backups() })

  const [diffFor, setDiffFor] = useState<string | null>(null)
  const [diffData, setDiffData] = useState<BackupDiffT[] | null>(null)
  const [contentFor, setContentFor] = useState<string | null>(null)

  const openDiff = async (ts: string) => {
    setDiffFor(ts)
    setDiffData(null)
    try {
      setDiffData(await api.backupDiff(ts))
    } catch (e: any) {
      push('error', e.message)
      setDiffFor(null)
    }
  }

  const restore = useMutation({
    mutationFn: (ts: string) => api.backupRestore(ts),
    onSuccess: () => {
      push('success', 'Backup restored, validated and services reloaded')
      qc.invalidateQueries({ queryKey: ['backups'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  const del = useMutation({
    mutationFn: (ts: string) => api.backupDelete(ts),
    onSuccess: () => {
      push('success', 'Backup deleted')
      qc.invalidateQueries({ queryKey: ['backups'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-bold">Config Backups</h1>
        <p className="text-xs text-muted-foreground">
          A timestamped snapshot of every config file is taken before each Apply. Restore is validated with the engine
          binary first and keeps a safety copy of the current state.
        </p>
      </div>

      <Card>
        <CardHeader title="Snapshots" desc={`${backups.data?.length ?? 0} backup folders`} />
        {backups.isLoading ? (
          <div className="flex justify-center py-16"><Spinner /></div>
        ) : !backups.data?.length ? (
          <Empty message="No backups yet — they are created automatically on every Apply." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">Timestamp</th>
                  <th className="px-3 py-2.5 font-medium">Engines</th>
                  <th className="px-3 py-2.5 font-medium">Files</th>
                  <th className="px-3 py-2.5 font-medium">Size</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {backups.data.map((b) => {
                  const engines = [...new Set(b.files.map((f) => f.engine).filter(Boolean))]
                  const size = b.files.reduce((acc, f) => acc + f.size, 0)
                  return (
                    <tr key={b.timestamp} className="hover:bg-muted/60">
                      <td className="px-5 py-3 font-mono text-xs">{b.timestamp}</td>
                      <td className="px-3 py-3">
                        {engines.map((e) => (
                          <Badge key={e} color={e === 'nginx' ? 'green' : 'purple'}>{e}</Badge>
                        ))}
                      </td>
                      <td className="px-3 py-3 text-xs">{b.files.length}</td>
                      <td className="px-3 py-3 text-xs">{fmtBytes(size)}</td>
                      <td className="px-5 py-3">
                        <div className="flex justify-end gap-1">
                          <Button size="sm" variant="ghost" title="View diff vs live" onClick={() => openDiff(b.timestamp)}>
                            <GitCompare className="h-3.5 w-3.5" />
                          </Button>
                          <Button size="sm" variant="secondary" disabled={restore.isPending}
                            onClick={() => { if (confirm(`Restore backup ${b.timestamp}? The current config is backed up first.`)) restore.mutate(b.timestamp) }}>
                            <RotateCcw className="h-3.5 w-3.5" /> Restore
                          </Button>
                          <Button size="sm" variant="ghost" className="text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20"
                            onClick={() => { if (confirm(`Delete backup ${b.timestamp}?`)) del.mutate(b.timestamp) }}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {/* Diff modal */}
      <Modal open={diffFor !== null} onClose={() => setDiffFor(null)} wide
        title={contentFor ? '' : `Diff: backup ${diffFor} vs live`}>
        {diffData === null ? (
          <div className="flex justify-center py-10"><Spinner /></div>
        ) : (
          <div className="space-y-3">
            <DiffView diffs={diffData} />
            <div className="flex justify-end border-t border-border pt-3">
              <Button variant="secondary" onClick={() => setDiffFor(null)}>Close</Button>
            </div>
          </div>
        )}
      </Modal>

      {/* content placeholder modal is unused; keep for future file viewer */}
      <Modal open={contentFor !== null} onClose={() => setContentFor(null)} title="File">
        <FileText className="h-4 w-4" />
      </Modal>
    </div>
  )
}
