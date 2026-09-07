import { Plus, RefreshCw, Trash2, Zap } from 'lucide-react'
import { Badge, Button, Empty, Field, Input, Modal, Spinner } from '../ui'
import { CodeEditor } from '../ui'
import type { ServerNode } from '../../api'

/** Manage modal: drive one node's mappings + certs remotely. All data and
 *  mutations are owned by the page; this is presentation only. */
export default function ManageModal({
  node, tab, onTab, loading, mappings, certs, newJSON, setNewJSON, jsonInvalid,
  onClose, onReload, onApply, onCreate, onDeleteMapping, onJsonValid,
}: {
  node: ServerNode | null
  tab: 'mappings' | 'certs'
  onTab: (t: 'mappings' | 'certs') => void
  loading: boolean
  mappings: any[] | null
  certs: any[] | null
  newJSON: string
  setNewJSON: (v: string) => void
  jsonInvalid: boolean
  onClose: () => void
  onReload: () => void
  onApply: () => void
  onCreate: () => void
  onDeleteMapping: (mid: number) => void
  onJsonValid: (ok: boolean) => void
}) {
  return (
    <Modal open={node !== null} onClose={onClose} wide
      title={node ? `Manage — ${node.name} (${node.host}:${node.port})` : ''}>
      {node && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex gap-1 rounded-xl border border-border bg-mutedslate p-1">
              {(['mappings', 'certs'] as const).map((t) => (
                <button
                  key={t}
                  type="button"
                  onClick={() => onTab(t)}
                  className={`rounded-lg px-3 py-1.5 text-xs font-medium capitalize ${
                    tab === t ? 'bg-background text-primary shadow-sm' : 'text-muted-foreground'
                  }`}
                >
                  {t}
                </button>
              ))}
            </div>
            <div className="flex gap-2">
              <Button variant="secondary" size="sm" onClick={onReload}><RefreshCw className="h-3.5 w-3.5" /></Button>
              <Button variant="success" size="sm" onClick={onApply}><Zap className="h-3.5 w-3.5" /> Apply on node</Button>
            </div>
          </div>

          {loading ? (
            <div className="flex justify-center py-12"><Spinner /></div>
          ) : tab === 'mappings' ? (
            <>
              {(mappings ?? []).length === 0 ? (
                <Empty message="No mappings on this node yet." />
              ) : (
                <div className="max-h-64 overflow-auto rounded-xl border border-border">
                  <table className="w-full text-sm">
                    <tbody className="divide-y divide-border">
                      {(mappings ?? []).map((m: any) => (
                        <tr key={m.id}>
                          <td className="px-4 py-2.5 font-medium">{m.name}</td>
                          <td className="px-3 py-2.5"><Badge color={m.engine === 'nginx' ? 'green' : 'purple'}>{m.engine}</Badge></td>
                          <td className="px-3 py-2.5"><Badge color="slate">{m.protocol}</Badge></td>
                          <td className="px-3 py-2.5 font-mono text-xs">{m.listen_ip}:{m.listen_port}</td>
                          <td className="px-3 py-2.5 text-xs text-muted-foreground">{m.targets?.map((t: any) => `${t.host}:${t.port}`).join(', ') || (m.path_routes?.length ? 'path routes' : '—')}</td>
                          <td className="px-3 py-2.5 text-right">
                            <Button variant="ghost" size="sm" className="text-red-500" onClick={() => onDeleteMapping(m.id)}>
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <div>
                <div className="mb-1.5 text-xs font-medium text-muted-foreground">
                  Create mapping (raw JSON — same shape as the local editor)
                </div>
                <CodeEditor
                  rows={7}
                  value={newJSON}
                  onChange={setNewJSON}
                  onValidate={(ok) => onJsonValid(ok)}
                  invalid={jsonInvalid}
                  placeholder={`{\n  "name": "web",\n  "enabled": true,\n  "engine": "nginx",\n  "protocol": "http",\n  "listen_ip": "0.0.0.0",\n  "listen_port": 8081,\n  "server_names": ["example.com"],\n  "targets": [{ "host": "127.0.0.1", "port": 3000 }]\n}`}
                />
                <div className="mt-2 flex justify-end">
                  <Button size="sm" onClick={onCreate} disabled={!newJSON.trim() || jsonInvalid}>
                    <Plus className="h-3.5 w-3.5" /> Create on node
                  </Button>
                </div>
              </div>
            </>
          ) : (
            <div className="space-y-3">
              {(certs ?? []).length === 0 ? (
                <Empty message="No certificates on this node." />
              ) : (
                <div className="max-h-48 overflow-auto rounded-xl border border-border">
                  <table className="w-full text-sm">
                    <tbody className="divide-y divide-border">
                      {(certs ?? []).map((c: any) => (
                        <tr key={c.id}>
                          <td className="px-4 py-2.5 font-medium">{c.name}</td>
                          <td className="px-3 py-2.5"><Badge color={c.type === 'manual' ? 'blue' : 'amber'}>{c.type}</Badge></td>
                          <td className="px-3 py-2.5 text-xs text-muted-foreground">{(c.domains ?? []).join(', ')}</td>
                          <td className="px-3 py-2.5 text-2xs text-muted-foreground">
                            {c.expires_at ? new Date(c.expires_at).toLocaleDateString() : '—'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <p className="text-2xs leading-relaxed text-muted-foreground">
                Upload certs for this node with the API:
                <code> POST /api/nodes/{node?.id}/certs</code> with
                <code> name / cert_pem / key_pem / domains</code>. The apply pipeline writes
                <code> .crt/.key</code> files on the node automatically.
              </p>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}
