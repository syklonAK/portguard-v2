import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { History, Download, RotateCcw, GitCompare, CheckCircle2, XCircle, Undo2 } from 'lucide-react'
import { api, type ConfigVersion } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Modal, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

function resultBadge(res: string) {
  if (res === 'ok') return <Badge color="green"><CheckCircle2 className="h-3 w-3" /> ok</Badge>
  if (res.startsWith('rolled back')) return <Badge color="amber"><Undo2 className="h-3 w-3" /> rolled back</Badge>
  if (res.startsWith('error')) return <Badge color="red"><XCircle className="h-3 w-3" /> error</Badge>
  if (!res) return <span className="text-2xs text-muted-foreground">—</span>
  return <Badge color="slate">{res}</Badge>
}

export default function Versions() {
  const qc = useQueryClient()
  const { push } = useToast()

  const versions = useQuery({ queryKey: ['versions'], queryFn: () => api.listVersions() })
  const [diffModal, setDiffModal] = useState<{ from: number; to: number; lines?: string[] } | null>(null)
  const [viewModal, setViewModal] = useState<{ v: number; mappings: any[]; relays: any[] } | null>(null)

  const restore = useMutation({
    mutationFn: (v: number) => api.restoreVersion(v),
    onSuccess: (res) => {
      push('success', `Restored ${res.restored_mappings} mappings — apply to activate.`)
      qc.invalidateQueries({ queryKey: ['versions'] })
      qc.invalidateQueries({ queryKey: ['mappings'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  const openView = async (v: number) => {
    try {
      const res = await api.getVersion(v)
      setViewModal({ v, mappings: res.mappings ?? [], relays: res.relays ?? [] })
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const openDiff = async (from: number, to: number) => {
    try {
      const res = await api.diffVersions(from, to)
      setDiffModal({ from, to, lines: res.diff ?? [] })
    } catch (e: any) {
      push('error', e.message)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Configuration Versions</h1>
          <p className="text-xs text-muted-foreground">
            Every apply records an immutable snapshot — view, compare, download or restore any version.
            Restores are snapshotted too, so they can be undone.
          </p>
        </div>
      </div>

      <Card>
        <CardHeader title="Version history" desc={`${versions.data?.length ?? 0} versions (50 most recent kept)`} />
        {versions.isLoading ? (
          <div className="flex justify-center py-14"><Spinner /></div>
        ) : !versions.data?.length ? (
          <Empty message="No versions yet — press Apply once and a snapshot appears here." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">Version</th>
                  <th className="px-3 py-2.5 font-medium">When</th>
                  <th className="px-3 py-2.5 font-medium">Author</th>
                  <th className="px-3 py-2.5 font-medium">Change</th>
                  <th className="px-3 py-2.5 font-medium">Deploy</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {versions.data.map((v, i) => (
                  <tr key={v.version} className="hover:bg-muted/60">
                    <td className="px-5 py-2.5 font-mono font-medium">v{v.version}</td>
                    <td className="px-3 py-2.5 text-xs text-muted-foreground">{new Date(v.created_at).toLocaleString()}</td>
                    <td className="px-3 py-2.5 text-xs">{v.author}</td>
                    <td className="px-3 py-2.5 text-xs">{v.description}</td>
                    <td className="px-3 py-2.5">{resultBadge(v.deploy_result)}</td>
                    <td className="px-5 py-2.5">
                      <div className="flex justify-end gap-1">
                        {i < versions.data.length - 1 && (
                          <Button variant="ghost" size="sm" title="Diff against previous"
                            onClick={() => openDiff(versions.data![i + 1].version, v.version)}>
                            <GitCompare className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        <Button variant="ghost" size="sm" title="View snapshot" onClick={() => openView(v.version)}>
                          <History className="h-3.5 w-3.5" />
                        </Button>
                        <a href={api.downloadVersion(v.version)} download
                          className="rounded-md p-1.5 text-muted-foreground hover:bg-muted/60" title="Download">
                          <Download className="h-3.5 w-3.5" />
                        </a>
                        <Button variant="ghost" size="sm" className="text-red-500" title="Restore this version"
                          onClick={() => restore.mutate(v.version)} disabled={restore.isPending}>
                          <RotateCcw className="h-3.5 w-3.5" />
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

      {/* diff modal */}
      <Modal open={diffModal !== null} onClose={() => setDiffModal(null)} wide
        title={diffModal ? `Diff — v${diffModal.from} → v${diffModal.to}` : ''}>
        {diffModal && (
          <div className="space-y-1 font-mono text-xs">
            {diffModal.lines?.length === 0 && (
              <p className="py-6 text-center text-muted-foreground">No changes between these versions.</p>
            )}
            {(diffModal.lines ?? []).map((line, i) => (
              <div key={i} className={
                line.startsWith('+') && !line.startsWith('+++') ? 'text-emerald-400' :
                line.startsWith('-') && !line.startsWith('---') ? 'text-red-400' :
                line.startsWith('@@') ? 'text-sky-400' :
                'text-muted-foreground'
              }>{line}</div>
            ))}
          </div>
        )}
      </Modal>

      {/* snapshot view modal */}
      <Modal open={viewModal !== null} onClose={() => setViewModal(null)} wide
        title={viewModal ? `Snapshot v${viewModal.v}` : ''}>
        {viewModal && (
          <div className="space-y-3">
            <p className="text-2xs text-muted-foreground">{viewModal.mappings.length} mappings, {viewModal.relays.length} tunnel relays</p>
            <div className="max-h-72 overflow-auto rounded-xl border border-border">
              <table className="w-full text-xs">
                <tbody className="divide-y divide-border">
                  {viewModal.mappings.map((m) => (
                    <tr key={m.id}>
                      <td className="px-3 py-2 font-medium">{m.name}</td>
                      <td className="px-3 py-2">{m.engine}</td>
                      <td className="px-3 py-2">{m.protocol}</td>
                      <td className="px-3 py-2 font-mono">{m.listen_ip}:{m.listen_port}</td>
                      <td className="px-3 py-2">{m.enabled ? <Badge color="green">on</Badge> : <Badge color="slate">off</Badge>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}
