import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, Pencil, Boxes, ChevronRight } from 'lucide-react'
import { api, type ServiceView, type Service, type Mapping } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Field, Input, Modal, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'

export default function Services() {
  const qc = useQueryClient()
  const { push } = useToast()

  const services = useQuery({ queryKey: ['services'], queryFn: () => api.listServices() })
  const [modal, setModal] = useState<null | 'create' | 'edit'>()
  const [draft, setDraft] = useState<Partial<Service>>({})
  const [saving, setSaving] = useState(false)
  const [detail, setDetail] = useState<{ service: Service; mappings: Mapping[] } | null>(null)

  const refresh = () => qc.invalidateQueries({ queryKey: ['services'] })

  const save = async () => {
    setSaving(true)
    try {
      if (modal === 'create') {
        await api.createService({ ...draft, enabled: draft.enabled ?? true })
        push('success', 'Service created. Assign mappings to it in the mapping editor.')
      } else if (modal === 'edit' && draft.id) {
        await api.updateService(draft.id, draft)
        push('success', 'Service updated.')
      }
      setModal(null)
      refresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setSaving(false)
    }
  }

  const del = useMutation({
    mutationFn: (id: number) => api.deleteService(id),
    onSuccess: () => {
      push('success', 'Service deleted. Its mappings are kept and unassigned.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const openDetail = async (s: ServiceView) => {
    try {
      const res = await api.serviceDetail(s.id)
      setDetail({ service: res.service, mappings: res.mappings ?? [] })
    } catch (e: any) {
      push('error', e.message)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Services</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Logical groups of mappings — deploy, monitor and roll back one service at a time.
          </p>
        </div>
        <Button onClick={() => { setDraft({ name: '', description: '', enabled: true }); setModal('create') }}>
          <Plus className="h-4 w-4" /> New service
        </Button>
      </div>

      {services.isLoading ? (
        <div className="flex justify-center py-14"><Spinner /></div>
      ) : !services.data?.length ? (
        <Empty message="No services yet. Create one, then assign mappings to it from the mapping editor." />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {services.data.map((s) => (
            <Card key={s.id} className={`flex flex-col ${!s.enabled ? 'opacity-60' : ''}`}>
              <div className="flex items-start justify-between p-4 pb-2">
                <div className="flex items-center gap-2.5">
                  <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-indigo-100 text-indigo-600 dark:bg-indigo-500/15 dark:text-indigo-300">
                    <Boxes className="h-4.5 w-4.5" />
                  </div>
                  <div>
                    <div className="text-sm font-semibold">{s.name}</div>
                    <div className="text-2xs text-slate-400">{s.description || '—'}</div>
                  </div>
                </div>
                <div className="flex gap-1">
                  <Button variant="ghost" size="sm" onClick={() => { setDraft(s); setModal('edit') }}><Pencil className="h-3.5 w-3.5" /></Button>
                  <Button variant="ghost" size="sm" className="text-red-500" onClick={() => del.mutate(s.id)}><Trash2 className="h-3.5 w-3.5" /></Button>
                </div>
              </div>
              <div className="grid grid-cols-3 gap-2 px-4 pb-3 text-center">
                <div className="rounded-lg bg-slate-50 p-2 dark:bg-slate-800/60">
                  <div className="text-sm font-bold">{s.mapping_count}</div>
                  <div className="text-2xs text-slate-400">mappings</div>
                </div>
                <div className="rounded-lg bg-slate-50 p-2 dark:bg-slate-800/60">
                  <div className="text-sm font-bold text-emerald-500">{s.backends_up}</div>
                  <div className="text-2xs text-slate-400">up</div>
                </div>
                <div className="rounded-lg bg-slate-50 p-2 dark:bg-slate-800/60">
                  <div className={`text-sm font-bold ${s.backends_down ? 'text-red-500' : 'text-slate-400'}`}>{s.backends_down}</div>
                  <div className="text-2xs text-slate-400">down</div>
                </div>
              </div>
              <div className="mt-auto flex items-center justify-between border-t border-slate-200 px-4 py-2.5 dark:border-slate-800">
                <Toggle checked={s.enabled} onChange={async () => {
                  try { await api.updateService(s.id, { ...s, enabled: !s.enabled }); refresh() }
                  catch (e: any) { push('error', e.message) }
                }} />
                <button className="flex items-center gap-1 text-2xs font-medium text-indigo-500 hover:text-indigo-600"
                  onClick={() => openDetail(s)}>
                  Details <ChevronRight className="h-3 w-3" />
                </button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal open={modal != null} onClose={() => setModal(null)} title={modal === 'create' ? 'New service' : `Edit — ${draft.name}`}>
        <div className="space-y-4">
          <Field label="Name"><Input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder="xray-service" /></Field>
          <Field label="Description"><Input value={draft.description || ''} onChange={(e) => setDraft({ ...draft, description: e.target.value })} placeholder="Main Xray endpoints" /></Field>
          <label className="flex items-center gap-2 text-xs font-medium text-slate-600 dark:text-slate-300">
            <Toggle checked={draft.enabled ?? true} onChange={(v) => setDraft({ ...draft, enabled: v })} /> Enabled
          </label>
          <div className="flex justify-end gap-2 border-t border-slate-200 pt-4 dark:border-slate-700">
            <Button variant="secondary" onClick={() => setModal(null)}>Cancel</Button>
            <Button onClick={save} disabled={saving || !draft.name}>{saving ? 'Saving…' : 'Save service'}</Button>
          </div>
        </div>
      </Modal>

      <Modal open={detail !== null} onClose={() => setDetail(null)} wide
        title={detail ? `${detail.service.name} — member mappings` : ''}>
        {detail && (
          <div className="space-y-3">
            {!detail.mappings.length ? (
              <Empty message="No mappings assigned yet. Open a mapping in the editor and pick this service under Advanced." />
            ) : (
              <div className="max-h-72 overflow-auto rounded-xl border border-slate-200 dark:border-slate-700">
                <table className="w-full text-xs">
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {detail.mappings.map((m) => (
                      <tr key={m.id}>
                        <td className="px-3 py-2 font-medium">{m.name}</td>
                        <td className="px-3 py-2"><Badge color={m.engine === 'nginx' ? 'green' : 'purple'}>{m.engine}</Badge></td>
                        <td className="px-3 py-2"><Badge color="slate">{m.protocol}</Badge></td>
                        <td className="px-3 py-2 font-mono">:{m.listen_port}</td>
                        <td className="px-3 py-2">{m.enabled ? <Badge color="green">on</Badge> : <Badge color="slate">off</Badge>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  )
}
