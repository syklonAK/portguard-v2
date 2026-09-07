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

const NGINX_HTTP_BALANCE = [
  ['', 'Default (websocket → ip_hash, else least_conn)'],
  ['round_robin', 'Round Robin'],
  ['least_conn', 'Least Connections'],
  ['ip_hash', 'IP Hash (sticky by client IP)'],
  ['random', 'Random'],
] as const
const NGINX_STREAM_BALANCE = [
  ['', 'Round Robin (default)'],
  ['least_conn', 'Least Connections'],
  ['random', 'Random'],
] as const
const HAPROXY_BALANCE = [
  ['', 'Round Robin (default)'],
  ['roundrobin', 'Round Robin'],
  ['leastconn', 'Least Connections'],
  ['source', 'Source IP hash'],
  ['uri', 'URI hash'],
  ['random', 'Random'],
  ['first', 'First available'],
  ['static-rr', 'Static Round Robin'],
] as const

const TRANSPORT_META: Record<PathTransport, { label: string; hint: string; color: 'green' | 'blue' | 'purple' }> = {
  ws: { label: 'WebSocket', hint: 'Requires Upgrade header; tunnel-style forwarding', color: 'green' },
  httpupgrade: { label: 'HTTPUpgrade', hint: 'Like ws but pure HTTP upgrade without RFC 6455 frames', color: 'blue' },
  xhttp: { label: 'XHTTP', hint: 'Split HTTP streaming; no Upgrade, buffering off', color: 'purple' },
}

function DiffView({ diff }: { diff: string }) {
  return (
    <pre className="max-h-72 overflow-auto rounded-lg bg-slate-950 p-3 font-mono text-2xs leading-relaxed">
      {diff.split('\n').map((line, i) => (
        <div key={i} className={
          line.startsWith('+') ? 'text-emerald-400' :
          line.startsWith('-') ? 'text-red-400' :
          line.startsWith('@@') ? 'text-sky-400' : 'text-slate-400'
        }>{line || ' '}</div>
      ))}
    </pre>
  )
}

// ---------- Route rules editor (advanced routing) ----------

function RouteRulesEditor({ rules, onChange }: { rules: RouteRule[]; onChange: (r: RouteRule[]) => void }) {
  const update = (i: number, patch: Partial<RouteRule>) =>
    onChange(rules.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  const move = (i: number, dir: -1 | 1) => {
    const next = [...rules]
    const j = i + dir
    if (j < 0 || j >= next.length) return
    ;[next[i], next[j]] = [next[j], next[i]]
    onChange(next)
  }
  const add = () =>
    onChange([...rules, { id: Date.now(), path: '/api/*', enabled: true, targets: [{ host: '127.0.0.1', port: 8080 }] }])
  const clone = (i: number) => {
    const next = [...rules]
    next.splice(i + 1, 0, { ...rules[i], id: Date.now() + Math.floor(Math.random() * 1000) })
    onChange(next)
  }

  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-xs font-medium text-slate-600 dark:text-slate-300">
          Route rules <span className="text-2xs text-slate-400">— ordered path → target rules; mapping targets are the /* fallback</span>
        </span>
        <Button variant="ghost" size="sm" onClick={add}><Plus className="h-3.5 w-3.5" /> Add rule</Button>
      </div>
      {rules.length === 0 ? (
        <div className="rounded-lg border border-dashed border-slate-300 px-4 py-3 text-center text-2xs text-slate-400 dark:border-slate-600">
          No route rules. Example: <b>/ws/* → node-01:10001</b>, <b>/xhttp/* → node-02:10002</b>, <b>/api/* → backend-api</b>.
        </div>
      ) : (
        <div className="space-y-2">
          {rules.map((r, i) => (
            <div key={r.id} className="rounded-lg border border-slate-200 bg-slate-50/50 p-2.5 dark:border-slate-700 dark:bg-slate-800/40">
              <div className="flex flex-wrap items-center gap-2">
                <div className="flex flex-col">
                  <button className="text-slate-400 hover:text-slate-600 disabled:opacity-30" disabled={i === 0}
                    onClick={() => move(i, -1)} title="Move up">▲</button>
                  <button className="text-slate-400 hover:text-slate-600 disabled:opacity-30" disabled={i === rules.length - 1}
                    onClick={() => move(i, 1)} title="Move down">▼</button>
                </div>
                <Input className="h-8 w-36 font-mono" value={r.path} placeholder="/api/*"
                  onChange={(e) => update(i, { path: e.target.value })} />
                <span className="text-slate-400">→</span>
                {r.redirect ? (
                  <Input className="h-8 flex-1" placeholder="https://new.example.com"
                    value={r.redirect} onChange={(e) => update(i, { redirect: e.target.value })} />
                ) : (
                  <div className="flex flex-1 flex-wrap gap-1.5">
                    {r.targets.map((t, ti) => (
                      <span key={ti} className="flex items-center gap-1">
                        <div className="w-28 shrink-0">
                          <Input className="h-8 font-mono" value={t.host}
                            onChange={(e) => update(i, { targets: r.targets.map((x, xi) => (xi === ti ? { ...x, host: e.target.value } : x)) })} />
                        </div>
                        <div className="w-16 shrink-0">
                          <Input className="h-8" type="number" value={t.port}
                            onChange={(e) => update(i, { targets: r.targets.map((x, xi) => (xi === ti ? { ...x, port: parseInt(e.target.value, 10) || 0 } : x)) })} />
                        </div>
                        {r.targets.length > 1 && (
                          <Button variant="ghost" size="sm" className="text-red-500"
                            onClick={() => update(i, { targets: r.targets.filter((_, xi) => xi !== ti) })}>
                            <Trash2 className="h-3 w-3" />
                          </Button>
                        )}
                      </span>
                    ))}
                    <Button variant="ghost" size="sm"
                      onClick={() => update(i, { targets: [...r.targets, { host: '127.0.0.1', port: 80 }] })}>
                      <Plus className="h-3 w-3" />
                    </Button>
                  </div>
                )}
                <span className="flex-1" />
                <label className="flex items-center gap-1 text-2xs text-slate-500">
                  <input type="checkbox" checked={r.enabled} onChange={(e) => update(i, { enabled: e.target.checked })} /> on
                </label>
                <Button variant="ghost" size="sm" onClick={() => update(i, { redirect: r.redirect ? '' : 'https://' })}
                  title={r.redirect ? 'Switch to proxy' : 'Switch to redirect'}>{r.redirect ? '⇄' : '⇄'}</Button>
                <Button variant="ghost" size="sm" onClick={() => clone(i)} title="Clone"><Plus className="h-3 w-3" /></Button>
                <Button variant="ghost" size="sm" className="text-red-500"
                  onClick={() => onChange(rules.filter((_, idx) => idx !== i))}><Trash2 className="h-3 w-3" /></Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------- Path routes editor ----------

function PathRoutesEditor({ routes, onChange }: { routes: PathRoute[]; onChange: (r: PathRoute[]) => void }) {
  const update = (i: number, patch: Partial<PathRoute>) => onChange(routes.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  const remove = (i: number) => onChange(routes.filter((_, idx) => idx !== i))
  const add = () => onChange([...routes, { transport: 'ws', prefix: 'ws', min_port: 10000, max_port: 10003 }])

  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-xs font-medium text-slate-600 dark:text-slate-300">
          Path routes <span className="text-2xs text-slate-400">— /&lt;prefix&gt;/&lt;port&gt; → host:&lt;port&gt; (Xray-style dynamic forwarding)</span>
        </span>
        <Button variant="ghost" size="sm" onClick={add}><Plus className="h-3.5 w-3.5" /> Add transport</Button>
      </div>
      {routes.length === 0 ? (
        <div className="rounded-lg border border-dashed border-slate-300 px-4 py-3 text-center text-2xs text-slate-400 dark:border-slate-600">
          No path routes. Add <b>ws</b>, <b>httpupgrade</b> or <b>xhttp</b> to route by path with a dynamic port.
        </div>
      ) : (
        <div className="space-y-2">
          {routes.map((r, i) => (
            <div key={i} className="rounded-lg border border-slate-200 bg-slate-50/50 p-2.5 dark:border-slate-700 dark:bg-slate-800/40">
              <div className="flex flex-wrap items-center gap-2">
                <Select className="w-32" value={r.transport} onChange={(e) => update(i, { transport: e.target.value as PathTransport })}>
                  {(Object.keys(TRANSPORT_META) as PathTransport[]).map((t) => (
                    <option key={t} value={t}>{TRANSPORT_META[t].label}</option>
                  ))}
                </Select>
                <div className="flex items-center gap-1 text-2xs font-mono text-slate-400">
                  /<Input className="h-8 w-24 font-mono" placeholder="prefix" value={r.prefix} onChange={(e) => update(i, { prefix: e.target.value })} />/
                  <span className="rounded bg-indigo-100 px-1.5 py-0.5 font-bold text-indigo-700 dark:bg-indigo-500/20 dark:text-indigo-300">port</span>
                </div>
                <div className="ml-auto flex items-center gap-1">
                  <Input className="h-8 w-20" type="number" placeholder="min port" value={r.min_port || ''} onChange={(e) => update(i, { min_port: parseInt(e.target.value, 10) || 0 })} />
                  <span className="text-2xs text-slate-400">—</span>
                  <Input className="h-8 w-20" type="number" placeholder="max port" value={r.max_port || ''} onChange={(e) => update(i, { max_port: parseInt(e.target.value, 10) || 0 })} />
                  <Button variant="ghost" size="sm" className="text-red-500" onClick={() => remove(i)}><Trash2 className="h-3.5 w-3.5" /></Button>
                </div>
              </div>
              <p className="mt-1.5 text-2xs text-slate-400">{TRANSPORT_META[r.transport].hint}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------- Preview panel (shows what the config will look like) ----------

function RoutePreview({ draft }: { draft: Partial<Mapping> }) {
  const host = draft.targets?.[0]?.host || '127.0.0.1'
  const scheme = draft.protocol === 'https' ? 'https' : 'http'
  const needsNames = (draft.protocol === 'http' || draft.protocol === 'https') && !(draft.server_names || []).length
  const domain = draft.server_names?.[0] || `${draft.listen_ip || '0.0.0.0'}:${draft.listen_port}`
  const lines: string[] = []
  if (draft.path_routes?.length) {
    for (const r of draft.path_routes) {
      const rng = r.min_port && r.max_port ? `${r.min_port}-${r.max_port}` : 'any port'
      lines.push(`${scheme}://${domain}/${r.prefix}/<${rng}>  →  ${host}:<port>  [${TRANSPORT_META[r.transport]?.label || r.transport}]`)
    }
  }
  for (const r of draft.routes ?? []) {
    if (!r.enabled) continue
    if (r.redirect) {
      lines.push(`${scheme}://${domain}${r.path}  →  301 ${r.redirect}`)
    } else {
      lines.push(`${scheme}://${domain}${r.path}  →  ${r.targets.map((t) => `${t.host}:${t.port}`).join(', ')}`)
    }
  }
  if (draft.path_prefix) {
    lines.push(`${scheme}://${domain}${draft.path_prefix}  →  backend (prefix match, rest → 404)`)
  }
  if (draft.targets?.length && !draft.path_routes?.length) {
    for (const t of draft.targets) {
      lines.push(`${scheme}://${domain}  →  ${t.host}:${t.port}`)
    }
  }
  if (draft.redirect_to) {
    lines.push(`*  →  301 ${draft.redirect_to}`)
  }
  if (lines.length === 0) lines.push('— nothing routed yet —')
  return (
    <div className="rounded-lg border border-indigo-100 bg-indigo-50/50 p-3 dark:border-indigo-500/20 dark:bg-indigo-500/5">
      <div className="mb-1.5 text-2xs font-semibold uppercase tracking-wide text-indigo-500">Live preview</div>
      <div className="space-y-1 font-mono text-2xs text-slate-600 dark:text-slate-300">
        {lines.map((l, i) => <div key={i}>{l}</div>)}
      </div>
    </div>
  )
}

// ---------- Templates gallery ----------

function TemplateGallery({ templates, onPick }: { templates: MappingTemplate[]; onPick: (t: MappingTemplate) => void }) {
  return (
    <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
      {templates.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => onPick(t)}
          className="group rounded-xl border border-slate-200 bg-white p-3.5 text-left transition-all hover:border-indigo-300 hover:shadow-md
            dark:border-slate-700 dark:bg-slate-800/60 dark:hover:border-indigo-500/50"
        >
          <div className="mb-1 text-xs font-semibold text-slate-700 group-hover:text-indigo-600 dark:text-slate-200 dark:group-hover:text-indigo-300">{t.name}</div>
          <div className="text-2xs leading-relaxed text-slate-400">{t.description}</div>
          <div className="mt-2 flex flex-wrap gap-1">
            <Badge color={t.mapping.engine === 'nginx' ? 'green' : 'purple'}>{t.mapping.engine}</Badge>
            {t.mapping.protocol && <Badge color="slate">{t.mapping.protocol}</Badge>}
            {t.mapping.path_routes?.map((pr) => (
              <Badge key={pr.transport} color={TRANSPORT_META[pr.transport]?.color || 'blue'}>{pr.transport}</Badge>
            ))}
          </div>
        </button>
      ))}
    </div>
  )
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
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Mappings</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Route listeners to backends via nginx or HAProxy. Changes take effect after <b>Apply</b>.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => validate.mutate()} disabled={validate.isPending}>
            <FileDiff className="h-4 w-4" />
            Validate / Diff
          </Button>
          <Button variant="success" onClick={() => apply.mutate()} disabled={apply.isPending}>
            <Zap className={`h-4 w-4 ${apply.isPending ? 'animate-pulse' : ''}`} />
            {apply.isPending ? 'Applying…' : 'Apply'}
          </Button>
          <Button variant="secondary" onClick={runImportScan} title="Import existing nginx/haproxy configs">
            <Download className="h-4 w-4" />
            Import existing
          </Button>
          <Button
            onClick={() => {
              setDraft(emptyMapping())
              setFormTab('visual')
              setModal('create')
            }}
          >
            <Plus className="h-4 w-4" />
            New mapping
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader title="All mappings" desc={`${mappings.data?.length ?? 0} total`} />
        {mappings.isLoading ? (
          <div className="flex justify-center py-16"><Spinner /></div>
        ) : !mappings.data?.length ? (
          <Empty message="No mappings yet. Click “New mapping” to create your first route." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                  <th className="px-5 py-2.5 font-medium">Name</th>
                  <th className="px-3 py-2.5 font-medium">Engine</th>
                  <th className="px-3 py-2.5 font-medium">Protocol</th>
                  <th className="px-3 py-2.5 font-medium">Listen</th>
                  <th className="px-3 py-2.5 font-medium">Domains</th>
                  <th className="px-3 py-2.5 font-medium">Routes / Targets</th>
                  <th className="px-3 py-2.5 font-medium">Enabled</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
                {mappings.data.map((m) => (
                  <tr key={m.id} className={`hover:bg-slate-50 dark:hover:bg-slate-800/40 ${!m.enabled ? 'opacity-60' : ''}`}>
                    <td className="px-5 py-3 font-medium">
                      {m.name}
                      {m.websocket && <Badge color="blue">ws</Badge>}
                      {m.redirect_to && <Badge color="amber">redirect</Badge>}
                    </td>
                    <td className="px-3 py-3"><Badge color={m.engine === 'nginx' ? 'green' : 'purple'}>{m.engine}</Badge></td>
                    <td className="px-3 py-3"><Badge color="slate">{m.protocol}</Badge></td>
                    <td className="px-3 py-3 font-mono text-xs">{m.listen_ip}:{m.listen_port}</td>
                    <td className="px-3 py-3 text-xs text-slate-500">{m.server_names.join(', ') || '—'}</td>
                    <td className="px-3 py-3 text-xs">
                      {m.path_routes?.length ? (
                        <div className="flex flex-wrap items-center gap-1">
                          {m.path_routes.map((pr) => (
                            <span key={pr.transport} className="font-mono">
                              <Badge color={TRANSPORT_META[pr.transport]?.color || 'blue'}>/{pr.prefix}/</Badge>
                            </span>
                          ))}
                          <span className="text-slate-400">→ dynamic ports</span>
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
        <div className="space-y-4">
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
              <div className="grid grid-cols-2 gap-3">
                <Field label="Name">
                  <Input value={draft.name || ''} onChange={(e) => setDraftField('name', e.target.value)} placeholder="my-app" />
                </Field>
                <div className="grid grid-cols-2 gap-3">
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

              <div className="grid grid-cols-2 gap-3">
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
                    <span className="text-xs font-medium text-slate-600 dark:text-slate-300">
                      Decoy site (anti-DPI) <span className="text-2xs text-slate-400">— serve a real-looking website on unmatched paths instead of 404</span>
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
                      className="mt-2 w-full resize-y rounded-lg border border-slate-700 bg-slate-950 p-3 font-mono text-xs leading-relaxed text-slate-100 placeholder:text-slate-600 focus:outline-none focus:ring-2 focus:ring-indigo-500/40"
                    />
                  )}
                  {draft.decoy && (
                    <p className="mt-1 text-2xs text-slate-400">
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
                      <span className="text-xs font-medium text-slate-600 dark:text-slate-300">
                        Backend targets
                        {hasPathRoutes && <span className="ml-1 text-2xs text-slate-400">— first host is used by path routes</span>}
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
                          <label className="flex items-center gap-1 text-2xs text-slate-500">
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
              <div className="rounded-lg border border-slate-200 dark:border-slate-700">
                <button
                  type="button"
                  onClick={() => setShowAdvanced((v) => !v)}
                  className="flex w-full items-center justify-between px-3.5 py-2.5 text-xs font-medium text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200"
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
                  <div className="space-y-4 border-t border-slate-200 p-3.5 dark:border-slate-700">
                    <div className="flex flex-wrap items-end gap-6">
                      <label className="flex items-center gap-2 pb-1.5 text-xs font-medium text-slate-600 dark:text-slate-300">
                        <Toggle checked={!!draft.websocket} onChange={(v) => setDraftField('websocket', v)} /> WebSocket
                      </label>
                      {draft.protocol === 'https' && (
                        <label className="flex items-center gap-2 pb-1.5 text-xs font-medium text-slate-600 dark:text-slate-300">
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
                        <span className="text-xs font-medium text-slate-600 dark:text-slate-300">
                          Access list <span className="text-2xs text-slate-400">— first match wins; any “allow” denies everyone else</span>
                        </span>
                        <Button variant="ghost" size="sm" onClick={addRule}><Plus className="h-3.5 w-3.5" /> Add rule</Button>
                      </div>
                      {(draft.access_rules || []).length === 0 ? (
                        <p className="text-2xs text-slate-400">No rules — the listener accepts traffic from all sources.</p>
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
              <p className="text-2xs text-slate-400">
                Full mapping object — edit freely and press Save. Unknown fields are ignored; validation runs server-side.
              </p>
              <CodeEditor
                value={jsonText}
                onChange={(v) => setJsonText(v)}
                onValidate={(ok) => setJsonInvalid(!ok)}
                invalid={jsonInvalid}
                rows={16}
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
                <p className="text-xs leading-relaxed text-slate-500 dark:text-slate-400">
                  Found {importModal.mappings.length} importable server block(s). Everything imports <b>disabled</b> —
                  review, fix any gaps flagged below, enable what you want, then Apply. PortGuard never overwrites
                  your live config until you press Apply.
                </p>
                {importModal.mappings.length > 0 && (
                  <div className="max-h-64 overflow-auto rounded-xl border border-slate-200 dark:border-slate-700">
                    <table className="w-full text-xs">
                      <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
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
                            <td className="px-3 py-2 text-slate-500">
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
                  {v.ok && !v.diff && <span className="text-2xs text-slate-400">no changes</span>}
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
