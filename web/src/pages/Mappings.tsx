import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import {
  Plus, Pencil, Trash2, Zap, Power, FileDiff, Wand2, Route, Braces, Sparkles, ChevronDown, ChevronUp, Download,
} from 'lucide-react'
import {
  api, type Mapping, type Target, type ACLRule, type PathRoute, type PathTransport,
  type MappingTemplate, type EngineValidation, type ImportScan, type RouteRule,
} from '../api'
import {
  Badge, Button, Card, CardHeader, CodeBlock, CodeEditor, Empty, Field, Input, Modal, Select,
  Spinner, Tabs, Toggle,
} from '../components/ui'
import { useToast } from '../components/toast'
import { NGINX_HTTP_BALANCE, NGINX_STREAM_BALANCE, HAPROXY_BALANCE, TRANSPORT_META, DiffView, RouteRulesEditor, PathRoutesEditor, RoutePreview, TemplateGallery } from '../components/mappings/editors'

function emptyMapping(prePort?: number): Partial<Mapping> {
  return {
    name: '',
    enabled: true,
    engine: 'nginx',
    protocol: 'http',
    listen_ip: '0.0.0.0',
    listen_port: prePort ?? 8081,
    server_names: [],
    ssl_cert_id: null,
    redirect_to: '',
    websocket: false,
    http2: true,
    targets: [{ host: '127.0.0.1', port: 3000 }],
    balance: '',
    path_prefix: '',
    access_rules: [],
    extra_headers: {},
    path_routes: [],
    routes: [],
    service_id: null,
    host_header: '',
    decoy: '',
    decoy_html: '',
    notes: '',
  }
}

export default function Mappings() {
  const qc = useQueryClient()
  const { push } = useToast()
  const [params, setParams] = useSearchParams()

  const mappings = useQuery({ queryKey: ['mappings'], queryFn: () => api.listMappings() })
  const certs = useQuery({ queryKey: ['certs'], queryFn: () => api.listCerts() })
  const templates = useQuery({ queryKey: ['templates'], queryFn: () => api.templates() })
  const services = useQuery({ queryKey: ['services'], queryFn: () => api.listServices() })

  const [modal, setModal] = useState<null | 'create' | 'edit'>(null)
  const [draft, setDraft] = useState<Partial<Mapping>>(emptyMapping())
  const [saving, setSaving] = useState(false)
  const [diffModal, setDiffModal] = useState<Record<string, EngineValidation> | null>(null)
  const [formTab, setFormTab] = useState('visual')
  const [jsonText, setJsonText] = useState('')
  const [jsonInvalid, setJsonInvalid] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [importModal, setImportModal] = useState<ImportScan | null>(null)
  const [importChecked, setImportChecked] = useState<number[]>([])
  const [importing, setImporting] = useState(false)

  const runImportScan = async () => {
    try {
      const res = await api.importScan()
      setImportModal(res)
      setImportChecked(res.mappings.map((_, i) => i))
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const confirmImport = async () => {
    if (!importModal) return
    setImporting(true)
    try {
      const res = await api.importConfirm(importChecked)
      push(res.created ? 'success' : 'warning',
        `Imported ${res.created} mapping(s)${res.skipped ? `, ${res.skipped} skipped` : ''} — all disabled. Review then Apply.`)
      setImportModal(null)
      refresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setImporting(false)
    }
  }

  // prefill from ports page (?new=1&port=N)
  useEffect(() => {
    if (params.get('new') === '1') {
      const port = parseInt(params.get('port') || '', 10)
      setDraft(emptyMapping(Number.isFinite(port) && port > 0 ? port : undefined))
      setFormTab('visual')
      setModal('create')
      setParams({}, { replace: true })
    }
  }, [params, setParams])

  // sync JSON tab text when opening the modal / switching tabs
  useEffect(() => {
    if (modal) setJsonText(JSON.stringify(draft, null, 2))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modal, formTab])

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['mappings'] })
    qc.invalidateQueries({ queryKey: ['system'] })
  }

  const applyJson = (): Partial<Mapping> | null => {
    try {
      const parsed = JSON.parse(jsonText) as Partial<Mapping>
      delete (parsed as Record<string, unknown>).id
      delete (parsed as Record<string, unknown>).created_at
      delete (parsed as Record<string, unknown>).updated_at
      const merged = { ...draft, ...parsed }
      setDraft(merged)
      return merged
    } catch {
      push('error', 'Invalid JSON — fix the syntax and try again')
      return null
    }
  }

  const save = async () => {
    let payload = draft
    if (formTab === 'json') {
      const merged = applyJson()
      if (!merged) return
      payload = merged
    }
    setSaving(true)
    try {
      if (modal === 'create') {
        await api.createMapping(payload)
        push('success', 'Mapping created. Apply changes to activate it.')
      } else if (modal === 'edit' && draft.id) {
        await api.updateMapping(draft.id, payload)
        push('success', 'Mapping updated. Apply changes to activate it.')
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
    mutationFn: (id: number) => api.deleteMapping(id),
    onSuccess: () => {
      push('success', 'Mapping deleted. Apply changes to activate it.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const toggle = async (m: Mapping) => {
    try {
      await api.updateMapping(m.id, { ...m, enabled: !m.enabled })
      refresh()
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const apply = useMutation({
    mutationFn: () => api.apply(),
    onSuccess: () => {
      push('success', 'Configuration applied & services reloaded')
      refresh()
    },
    onError: (e: any) => push('error', `Apply failed (rolled back): ${e.message}`),
  })

  const validate = useMutation({
    mutationFn: () => api.validate(),
    onSuccess: (res) => {
      const allOk = Object.values(res).every((v) => v.ok)
      if (allOk) {
        const anyDiff = Object.values(res).some((v) => v.diff)
        push(anyDiff ? 'warning' : 'success', anyDiff ? 'All valid — review the diff before applying.' : 'All valid, no changes to apply.')
        setDiffModal(res)
      } else {
        push('error', 'Validation failed — nothing was applied.')
        setDiffModal(res)
      }
    },
    onError: (e: any) => push('error', e.message),
  })

  const setDraftField = <K extends keyof Mapping>(k: K, v: Mapping[K]) => setDraft((d) => ({ ...d, [k]: v }))
  const setTarget = (i: number, patch: Partial<Target>) =>
    setDraftField('targets', (draft.targets || []).map((t, idx) => (idx === i ? { ...t, ...patch } : t)))
  const addTarget = () => setDraftField('targets', [...(draft.targets || []), { host: '127.0.0.1', port: 80 }])
  const removeTarget = (i: number) => setDraftField('targets', (draft.targets || []).filter((_, idx) => idx !== i))
  const setRule = (i: number, patch: Partial<ACLRule>) =>
    setDraftField('access_rules', (draft.access_rules || []).map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  const addRule = () => setDraftField('access_rules', [...(draft.access_rules || []), { action: 'allow', value: '10.0.0.0/8' }])
  const removeRule = (i: number) => setDraftField('access_rules', (draft.access_rules || []).filter((_, idx) => idx !== i))

  const edit = (m: Mapping) => {
    const copy = JSON.parse(JSON.stringify(m)) as Partial<Mapping>
    copy.access_rules = m.access_rules ?? []
    copy.path_routes = m.path_routes ?? []
    setDraft(copy)
    setFormTab('visual')
    setModal('edit')
  }

  const applyTemplate = (tpl: MappingTemplate) => {
    setDraft({ ...emptyMapping(), ...JSON.parse(JSON.stringify(tpl.mapping)) })
    setFormTab('visual')
  }

  const isL7 = draft.protocol === 'http' || draft.protocol === 'https'
  const needsCert = draft.protocol === 'https'
  // the backend rejects http/https mappings without a domain - surface that
  // in the form instead of failing at save time with a transient toast
  const needsNames = isL7 && !(draft.server_names || []).length
  const isRedirect = !!draft.redirect_to
  const hasPathRoutes = (draft.path_routes?.length ?? 0) > 0
  const balanceOptions = useMemo(() => {
    if (draft.engine === 'haproxy') return HAPROXY_BALANCE
    if (draft.protocol === 'tcp' || draft.protocol === 'udp') return NGINX_STREAM_BALANCE
    return NGINX_HTTP_BALANCE
  }, [draft.engine, draft.protocol])

  return (
    <div className="space-y-4 short:space-y-2.5">
      <div className="pg-sticky-bar flex flex-wrap items-center justify-between gap-3 short:gap-2">
        <div className="min-w-0">
          <h1 className="text-lg font-bold short:text-base">Mappings</h1>
          <p className="text-xs text-muted-foreground short:hidden">
            Route listeners to backends via nginx or HAProxy. Changes take effect after <b>Apply</b>.
          </p>
        </div>
        <div className="flex gap-2 short:gap-1.5">
          <Button variant="secondary" onClick={() => validate.mutate()} disabled={validate.isPending}
            title="Validate / Diff" className="short:px-2.5">
            <FileDiff className="h-4 w-4" />
            <span className="short:hidden">Validate / Diff</span>
          </Button>
          <Button variant="success" onClick={() => apply.mutate()} disabled={apply.isPending}
            title="Apply configuration" className="short:px-2.5">
            <Zap className={`h-4 w-4 ${apply.isPending ? 'animate-pulse' : ''}`} />
            <span className="short:hidden">{apply.isPending ? 'Applying…' : 'Apply'}</span>
          </Button>
          <Button variant="secondary" onClick={runImportScan} title="Import existing nginx/haproxy configs" className="short:px-2.5">
            <Download className="h-4 w-4" />
            <span className="short:hidden">Import existing</span>
          </Button>
          <Button
            title="New mapping"
            className="short:px-2.5"
            onClick={() => {
              setDraft(emptyMapping())
              setFormTab('visual')
              setModal('create')
            }}
          >
            <Plus className="h-4 w-4" />
            <span className="short:hidden">New mapping</span>
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader title="All mappings" desc={`${mappings.data?.length ?? 0} total`} />
        {mappings.isLoading ? (
          <div className="flex justify-center py-16 short:py-8"><Spinner /></div>
        ) : !mappings.data?.length ? (
          <Empty message="No mappings yet. Click “New mapping” to create your first route." />
        ) : (
          <div className="overflow-x-auto">
            <table className="pg-dense w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">Name</th>
                  <th className="px-3 py-2.5 font-medium">Engine</th>
                  <th className="px-3 py-2.5 font-medium short:hidden">Protocol</th>
                  <th className="px-3 py-2.5 font-medium">Listen</th>
                  <th className="px-3 py-2.5 font-medium short:hidden">Domains</th>
                  <th className="px-3 py-2.5 font-medium">Routes / Targets</th>
                  <th className="px-3 py-2.5 font-medium">Enabled</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {mappings.data.map((m) => (
                  <tr key={m.id} className={`hover:bg-muted/60 ${!m.enabled ? 'opacity-60' : ''}`}>
                    <td className="max-w-56 truncate px-5 py-3 font-medium" title={m.name}>
                      {m.name}
                      {m.websocket && <Badge color="blue">ws</Badge>}
                      {m.redirect_to && <Badge color="amber">redirect</Badge>}
                    </td>
                    <td className="px-3 py-3"><Badge color={m.engine === 'nginx' ? 'green' : 'purple'}>{m.engine}</Badge></td>
                    <td className="px-3 py-3 short:hidden"><Badge color="slate">{m.protocol}</Badge></td>
                    <td className="px-3 py-3 font-mono text-xs">{m.listen_ip}:{m.listen_port}</td>
                    <td className="px-3 py-3 text-xs text-muted-foreground short:hidden">{m.server_names.join(', ') || '—'}</td>
                    <td className="px-3 py-3 text-xs">
                      {m.path_routes?.length ? (
                        <div className="flex flex-wrap items-center gap-1">
                          {m.path_routes.map((pr) => (
                            <span key={pr.transport} className="font-mono">
                              <Badge color={TRANSPORT_META[pr.transport]?.color || 'blue'}>/{pr.prefix}/</Badge>
                            </span>
                          ))}
                          <span className="text-muted-foreground">→ dynamic ports</span>
                        </div>
                      ) : m.redirect_to ? (
                        <span className="text-amber-500">→ {m.redirect_to}</span>
                      ) : (
                        m.targets.map((t) => `${t.host}:${t.port}`).join(', ') || '—'
                      )}
                    </td>
                    <td className="px-3 py-3"><Toggle checked={m.enabled} onChange={() => toggle(m)} /></td>
                    <td className="px-5 py-3">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="sm" onClick={() => edit(m)} title="Edit"><Pencil className="h-3.5 w-3.5" /></Button>
                        <Button variant="ghost" size="sm" onClick={() => del.mutate(m.id)} title="Delete"
                          className="text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20"><Trash2 className="h-3.5 w-3.5" /></Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Modal open={modal !== null} onClose={() => setModal(null)} wide
        title={modal === 'create' ? 'New mapping' : `Edit mapping #${draft.id}`}
        footer={formTab !== 'templates' ? (
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setModal(null)}>Cancel</Button>
            <Button
              onClick={save}
              disabled={saving || !draft.name || (formTab === 'json' && jsonInvalid) || needsNames}
              title={needsNames ? 'Add at least one domain under Server names' : undefined}
            >
              {saving ? 'Saving…' : 'Save mapping'}
            </Button>
          </div>
        ) : null}>
        <div className="space-y-4 short:space-y-2.5">
          <Tabs
            tabs={[
              { id: 'visual', label: 'Visual builder', icon: Wand2 },
              { id: 'json', label: 'Raw JSON', icon: Braces },
              ...(modal === 'create' ? [{ id: 'templates', label: 'Templates', icon: Sparkles }] : []),
            ]}
            active={formTab}
            onChange={(id) => {
              if (formTab === 'json' && id !== 'json') applyJson()
              if (id === 'json') setJsonText(JSON.stringify(draft, null, 2))
              setFormTab(id)
            }}
          />

          {formTab === 'templates' && templates.data && (
            <TemplateGallery templates={templates.data} onPick={(t) => { applyTemplate(t); push('info', `Template "${t.name}" loaded — adjust and save.`) }} />
          )}

          {formTab === 'visual' && (
            <>
              <div className="grid grid-cols-2 gap-3 short:grid-cols-4 short:gap-2">
                <Field label="Name">
                  <Input value={draft.name || ''} onChange={(e) => setDraftField('name', e.target.value)} placeholder="my-app" />
                </Field>
                {/* short:contents flattens this wrapper so Engine/Protocol
                    join the parent grid as direct cells in compact mode */}
                <div className="grid grid-cols-2 gap-3 short:contents">
                  <Field label="Engine">
                    <Select value={draft.engine} onChange={(e) => setDraftField('engine', e.target.value as any)}>
                      <option value="nginx">nginx</option>
                      <option value="haproxy">haproxy</option>
                    </Select>
                  </Field>
                  <Field label="Protocol">
                    <Select value={draft.protocol} onChange={(e) => setDraftField('protocol', e.target.value as any)}>
                      <option value="http">http</option>
                      <option value="https">https</option>
                      <option value="tcp">tcp</option>
                      <option value="udp">udp</option>
                    </Select>
                  </Field>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3 short:gap-2">
                <Field label="Listen IP" hint="0.0.0.0 = all interfaces">
                  <Input value={draft.listen_ip || ''} onChange={(e) => setDraftField('listen_ip', e.target.value)} />
                </Field>
                <Field label="Listen port">
                  <Input type="number" min={1} max={65535} value={draft.listen_port || ''} onChange={(e) => setDraftField('listen_port', parseInt(e.target.value, 10))} />
                </Field>
              </div>

              <RoutePreview draft={draft} />

              {isL7 && (
                <>
                  <Field
                    label="Server names / domains"
                    hint="Comma separated, e.g. example.com, www.example.com (use * for catch-all)"
                  >
                    <Input
                      value={(draft.server_names || []).join(', ')}
                      onChange={(e) => setDraftField('server_names', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                    />
                  </Field>
                  {needsNames && (
                    <p className="-mt-1 text-2xs text-amber-600 dark:text-amber-400">
                      Required: http/https mappings need at least one domain, otherwise nginx/HAProxy reject the config.
                    </p>
                  )}
                  {needsCert && (
                    <Field label="SSL certificate">
                      <Select
                        value={draft.ssl_cert_id ?? ''}
                        onChange={(e) => setDraftField('ssl_cert_id', e.target.value ? parseInt(e.target.value, 10) : null)}
                      >
                        <option value="">— select certificate —</option>
                        {certs.data?.map((c) => (
                          <option key={c.id} value={c.id}>{c.name} ({c.type})</option>
                        ))}
                      </Select>
                      {!certs.data?.length && (
                        <span className="mt-1 block text-2xs text-amber-500">No certificates yet — add one on the SSL Certs page.</span>
                      )}
                    </Field>
                  )}
                  {!isRedirect && (
                    <PathRoutesEditor routes={draft.path_routes || []} onChange={(r) => setDraftField('path_routes', r)} />
                  )}
                </>
              )}

              {isL7 && !hasPathRoutes && (
                <Field label="Redirect to (optional)" hint="If set, all requests are 301-redirected instead of proxied">
                  <Input value={draft.redirect_to || ''} onChange={(e) => setDraftField('redirect_to', e.target.value)} placeholder="https://new.example.com" />
                </Field>
              )}

              {isL7 && !isRedirect && (
                <Field label="Host header (optional)" hint="Sent to the backend instead of $host — needed when proxying to a foreign node that expects its own domain">
                  <Input value={draft.host_header || ''} onChange={(e) => setDraftField('host_header', e.target.value)} placeholder="node.example.com" />
                </Field>
              )}

              {isL7 && !isRedirect && draft.engine === 'nginx' && (
                <div>
                  <div className="mb-1.5 flex items-center justify-between">
                    <span className="text-xs font-medium text-muted-foreground">
                      Decoy site (anti-DPI) <span className="text-2xs text-muted-foreground">— serve a real-looking website on unmatched paths instead of 404</span>
                    </span>
                  </div>
                  <Select
                    value={draft.decoy || ''}
                    onChange={(e) => setDraftField('decoy', e.target.value as '' | 'builtin' | 'custom')}
                  >
                    <option value="">Off — unmatched paths get 404</option>
                    <option value="builtin">Builtin — realistic SaaS landing page</option>
                    <option value="custom">Custom — my own HTML</option>
                  </Select>
                  {draft.decoy === 'custom' && (
                    <textarea
                      rows={8}
                      spellCheck={false}
                      value={draft.decoy_html || ''}
                      onChange={(e) => setDraftField('decoy_html', e.target.value)}
                      placeholder="<!DOCTYPE html>… your camouflage page …"
                      className="mt-2 w-full resize-y rounded-lg border border-border bg-popover p-3 font-mono text-xs leading-relaxed text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  )}
                  {draft.decoy && (
                    <p className="mt-1 text-2xs text-muted-foreground">
                      Served from /var/lib/portguard/decoy/ — users (and DPI probes) that hit a path outside your ws/hu/xhttp routes see a normal website.
                    </p>
                  )}
                </div>
              )}

              {!isRedirect && (!isL7 || !hasPathRoutes) && (
                <>
                  {(draft.targets || []).length > 1 && (
                    <Field label="Load balancing" hint="How traffic is spread across the backends">
                      <Select value={draft.balance || ''} onChange={(e) => setDraftField('balance', e.target.value)}>
                        {balanceOptions.map(([v, label]) => (
                          <option key={v} value={v}>{label}</option>
                        ))}
                      </Select>
                    </Field>
                  )}

                  <div>
                    <div className="mb-1.5 flex items-center justify-between">
                      <span className="text-xs font-medium text-muted-foreground">
                        Backend targets
                        {hasPathRoutes && <span className="ml-1 text-2xs text-muted-foreground">— first host is used by path routes</span>}
                      </span>
                      <Button variant="ghost" size="sm" onClick={addTarget}><Plus className="h-3.5 w-3.5" /> Add</Button>
                    </div>
                    <div className="space-y-2">
                      {(draft.targets || []).map((t, i) => (
                        <div key={i} className="flex items-center gap-2">
                          <Input className="min-w-0 flex-1" placeholder="host" value={t.host} onChange={(e) => setTarget(i, { host: e.target.value })} />
                          {/* fixed widths live on wrapper divs: the Input base is w-full and a w-24 sibling loses the cascade fight */}
                          <div className="w-24 shrink-0">
                            <Input type="number" placeholder="port" value={t.port || ''} onChange={(e) => setTarget(i, { port: parseInt(e.target.value, 10) })} />
                          </div>
                          {(draft.engine === 'nginx' || isL7) && (
                            <div className="w-20 shrink-0">
                              <Input type="number" placeholder="weight" value={t.weight ?? ''} onChange={(e) => setTarget(i, { weight: parseInt(e.target.value, 10) || undefined })} />
                            </div>
                          )}
                          <label className="flex items-center gap-1 text-2xs text-muted-foreground">
                            <input type="checkbox" checked={!!t.backup} onChange={(e) => setTarget(i, { backup: e.target.checked })} /> backup
                          </label>
                          <Button variant="ghost" size="sm" onClick={() => removeTarget(i)} className="text-red-500"><Trash2 className="h-3.5 w-3.5" /></Button>
                        </div>
                      ))}
                    </div>
                  </div>
                </>
              )}

              {/* Advanced section */}
              <div className="rounded-lg border border-border">
                <button
                  type="button"
                  onClick={() => setShowAdvanced((v) => !v)}
                  className="flex w-full items-center justify-between px-3.5 py-2.5 text-xs font-medium text-muted-foreground hover:text-foreground dark:text-muted-foreground "
                >
                  <span className="flex items-center gap-1.5">
                    Advanced
                    {(draft.access_rules?.length ?? 0) > 0 || draft.path_prefix || (draft.targets?.length ?? 0) > 1 ? (
                      <Badge color="amber">{(draft.access_rules?.length ?? 0) + (draft.path_prefix ? 1 : 0)} active</Badge>
                    ) : null}
                  </span>
                  {showAdvanced ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                </button>
                {showAdvanced && (
                  <div className="space-y-4 border-t border-border p-3.5">
                    <div className="flex flex-wrap items-end gap-6">
                      <label className="flex items-center gap-2 pb-1.5 text-xs font-medium text-muted-foreground">
                        <Toggle checked={!!draft.websocket} onChange={(v) => setDraftField('websocket', v)} /> WebSocket
                      </label>
                      {draft.protocol === 'https' && (
                        <label className="flex items-center gap-2 pb-1.5 text-xs font-medium text-muted-foreground">
                          <Toggle checked={!!draft.http2} onChange={(v) => setDraftField('http2', v)} /> HTTP/2
                        </label>
                      )}
                      {isL7 && (
                        <div className="w-52">
                          <Field label="Path prefix (optional)" hint="Only this path is proxied; rest → 404">
                            <Input value={draft.path_prefix || ''} onChange={(e) => setDraftField('path_prefix', e.target.value)} placeholder="/api" />
                          </Field>
                        </div>
                      )}
                    </div>

                    {/* Service assignment + route rules (advanced routing) */}
                    {isL7 && (
                      <div className="space-y-2">
                        <Field label="Service (optional)" hint="group related mappings for one-glance health & deploy">
                          <Select
                            value={draft.service_id ?? ''}
                            onChange={(e) => setDraftField('service_id', e.target.value ? parseInt(e.target.value, 10) : null)}
                          >
                            <option value="">— unassigned —</option>
                            {(services.data ?? []).map((s) => (
                              <option key={s.id} value={s.id}>{s.name}</option>
                            ))}
                          </Select>
                        </Field>
                        <RouteRulesEditor
                          rules={draft.routes || []}
                          onChange={(r) => setDraftField('routes', r)}
                        />
                      </div>
                    )}

                    {/* Access control */}
                    <div>
                      <div className="mb-1.5 flex items-center justify-between">
                        <span className="text-xs font-medium text-muted-foreground">
                          Access list <span className="text-2xs text-muted-foreground">— first match wins; any “allow” denies everyone else</span>
                        </span>
                        <Button variant="ghost" size="sm" onClick={addRule}><Plus className="h-3.5 w-3.5" /> Add rule</Button>
                      </div>
                      {(draft.access_rules || []).length === 0 ? (
                        <p className="text-2xs text-muted-foreground">No rules — the listener accepts traffic from all sources.</p>
                      ) : (
                        <div className="space-y-2">
                          {(draft.access_rules || []).map((r, i) => (
                            <div key={i} className="flex items-center gap-2">
                              <Select className="w-28" value={r.action} onChange={(e) => setRule(i, { action: e.target.value as any })}>
                                <option value="allow">allow</option>
                                <option value="deny">deny</option>
                              </Select>
                              <Input className="flex-1 font-mono text-xs" placeholder="IP or CIDR, e.g. 10.0.0.0/8" value={r.value} onChange={(e) => setRule(i, { value: e.target.value })} />
                              <Button variant="ghost" size="sm" className="text-red-500" onClick={() => removeRule(i)}><Trash2 className="h-3.5 w-3.5" /></Button>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </div>

              <Field label="Notes (optional)">
                <Input value={draft.notes || ''} onChange={(e) => setDraftField('notes', e.target.value)} />
              </Field>
            </>
          )}

          {formTab === 'json' && (
            <div className="space-y-2">
              <p className="text-2xs text-muted-foreground">
                Full mapping object — edit freely and press Save. Unknown fields are ignored; validation runs server-side.
              </p>
              <CodeEditor
                value={jsonText}
                onChange={(v) => setJsonText(v)}
                onValidate={(ok) => setJsonInvalid(!ok)}
                invalid={jsonInvalid}
                rows={16}
                className="short:h-44"
                placeholder='{ "name": "..." }'
              />
              {jsonInvalid && <p className="text-2xs text-red-500">Invalid JSON syntax</p>}
            </div>
          )}
        </div>
      </Modal>

      {/* Import existing configs modal */}
      <Modal open={importModal !== null} onClose={() => setImportModal(null)} wide
        title="Import existing nginx / HAProxy configs"
        footer={importModal?.found ? (
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setImportModal(null)}>Cancel</Button>
            <Button onClick={confirmImport} disabled={importing || importChecked.length === 0}>
              {importing ? 'Importing…' : `Import ${importChecked.length} mapping(s)`}
            </Button>
          </div>
        ) : null}>
        {importModal && (
          <div className="space-y-4">
            {!importModal.found ? (
              <Empty message="No existing nginx/HAProxy configuration found on this server — nothing to import." />
            ) : (
              <>
                <p className="text-xs leading-relaxed text-muted-foreground">
                  Found {importModal.mappings.length} importable server block(s). Everything imports <b>disabled</b> —
                  review, fix any gaps flagged below, enable what you want, then Apply. PortGuard never overwrites
                  your live config until you press Apply.
                </p>
                {importModal.mappings.length > 0 && (
                  <div className="max-h-64 overflow-auto rounded-xl border border-border">
                    <table className="w-full text-xs">
                      <tbody className="divide-y divide-border">
                        {importModal.mappings.map((m, i) => (
                          <tr key={i}>
                            <td className="px-3 py-2">
                              <input
                                type="checkbox"
                                checked={importChecked.includes(i)}
                                onChange={(e) =>
                                  setImportChecked((prev) =>
                                    e.target.checked ? [...prev, i] : prev.filter((x) => x !== i)
                                  )
                                }
                              />
                            </td>
                            <td className="px-3 py-2 font-medium">{m.name}</td>
                            <td className="px-3 py-2">{m.engine}</td>
                            <td className="px-3 py-2">{m.protocol}</td>
                            <td className="px-3 py-2 font-mono">:{m.listen_port}</td>
                            <td className="px-3 py-2 text-muted-foreground">
                              {(m as any).targets?.map((t: any) => `${t.host}:${t.port}`).join(', ') || (m as any).redirect_to || '—'}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
                {importModal.issues.length > 0 && (
                  <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-2xs leading-relaxed text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
                    {importModal.issues.map((iss, i) => (
                      <div key={i}>• <b>{iss.section}</b>: {iss.reason}</div>
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </Modal>

      {/* Validate / diff modal */}
      <Modal open={diffModal !== null} onClose={() => setDiffModal(null)} wide title="Dry run — staged configuration vs live"
        footer={diffModal ? (
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setDiffModal(null)}>Close</Button>
            <Button variant="success" disabled={!Object.values(diffModal).every((v) => v.ok)}
              onClick={() => { setDiffModal(null); apply.mutate() }}>
              <Wand2 className="h-4 w-4" /> Apply now
            </Button>
          </div>
        ) : null}>
        {diffModal && (
          <div className="space-y-4">
            {Object.entries(diffModal).map(([engine, v]) => (
              <div key={engine}>
                <div className="mb-1.5 flex items-center gap-2">
                  <Badge color={engine === 'nginx' ? 'green' : 'purple'}>{engine}</Badge>
                  {v.ok ? <Badge color="green">valid</Badge> : <Badge color="red">error</Badge>}
                  {!v.ok && <span className="text-2xs text-red-500">{v.error}</span>}
                  {v.ok && !v.diff && <span className="text-2xs text-muted-foreground">no changes</span>}
                </div>
                {v.ok && v.diff && <DiffView diff={v.diff} />}
              </div>
            ))}
          </div>
        )}
      </Modal>
    </div>
  )
}
