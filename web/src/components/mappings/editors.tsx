import { Plus, Trash2 } from 'lucide-react'
import {
  Badge, Button, Field, Input, Select, Toggle,
} from '../ui'
import type { Mapping, MappingTemplate, PathRoute, PathTransport, RouteRule } from '../../api'

export const NGINX_HTTP_BALANCE = [
  ['', 'Default (websocket → ip_hash, else least_conn)'],
  ['round_robin', 'Round Robin'],
  ['least_conn', 'Least Connections'],
  ['ip_hash', 'IP Hash (sticky by client IP)'],
  ['random', 'Random'],
] as const
export const NGINX_STREAM_BALANCE = [
  ['', 'Round Robin (default)'],
  ['least_conn', 'Least Connections'],
  ['random', 'Random'],
] as const
export const HAPROXY_BALANCE = [
  ['', 'Round Robin (default)'],
  ['roundrobin', 'Round Robin'],
  ['leastconn', 'Least Connections'],
  ['source', 'Source IP hash'],
  ['uri', 'URI hash'],
  ['random', 'Random'],
  ['first', 'First available'],
  ['static-rr', 'Static Round Robin'],
] as const

export const TRANSPORT_META: Record<PathTransport, { label: string; hint: string; color: 'green' | 'blue' | 'purple' }> = {
  ws: { label: 'WebSocket', hint: 'Requires Upgrade header; tunnel-style forwarding', color: 'green' },
  httpupgrade: { label: 'HTTPUpgrade', hint: 'Like ws but pure HTTP upgrade without RFC 6455 frames', color: 'blue' },
  xhttp: { label: 'XHTTP', hint: 'Split HTTP streaming; no Upgrade, buffering off', color: 'purple' },
}

export function DiffView({ diff }: { diff: string }) {
  return (
    <pre className="max-h-72 overflow-auto rounded-lg bg-popover p-3 font-mono text-2xs leading-relaxed">
      {diff.split('\n').map((line, i) => (
        <div key={i} className={
          line.startsWith('+') ? 'text-emerald-400' :
          line.startsWith('-') ? 'text-red-400' :
          line.startsWith('@@') ? 'text-sky-400' : 'text-muted-foreground'
        }>{line || ' '}</div>
      ))}
    </pre>
  )
}

// ---------- Route rules editor (advanced routing) ----------

export function RouteRulesEditor({ rules, onChange }: { rules: RouteRule[]; onChange: (r: RouteRule[]) => void }) {
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
        <span className="text-xs font-medium text-muted-foreground">
          Route rules <span className="text-2xs text-muted-foreground">— ordered path → target rules; mapping targets are the /* fallback</span>
        </span>
        <Button variant="ghost" size="sm" onClick={add}><Plus className="h-3.5 w-3.5" /> Add rule</Button>
      </div>
      {rules.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-3 text-center text-2xs text-muted-foreground">
          No route rules. Example: <b>/ws/* → node-01:10001</b>, <b>/xhttp/* → node-02:10002</b>, <b>/api/* → backend-api</b>.
        </div>
      ) : (
        <div className="space-y-2">
          {rules.map((r, i) => (
            <div key={r.id} className="rounded-lg border border-border bg-mutedslate p-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <div className="flex flex-col">
                  <button className="text-muted-foreground hover:text-muted-foreground disabled:opacity-30" disabled={i === 0}
                    onClick={() => move(i, -1)} title="Move up">▲</button>
                  <button className="text-muted-foreground hover:text-muted-foreground disabled:opacity-30" disabled={i === rules.length - 1}
                    onClick={() => move(i, 1)} title="Move down">▼</button>
                </div>
                <Input className="h-8 w-36 font-mono" value={r.path} placeholder="/api/*"
                  onChange={(e) => update(i, { path: e.target.value })} />
                <span className="text-muted-foreground">→</span>
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
                <label className="flex items-center gap-1 text-2xs text-muted-foreground">
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

export function PathRoutesEditor({ routes, onChange }: { routes: PathRoute[]; onChange: (r: PathRoute[]) => void }) {
  const update = (i: number, patch: Partial<PathRoute>) => onChange(routes.map((r, idx) => (idx === i ? { ...r, ...patch } : r)))
  const remove = (i: number) => onChange(routes.filter((_, idx) => idx !== i))
  const add = () => onChange([...routes, { transport: 'ws', prefix: 'ws', min_port: 10000, max_port: 10003 }])

  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-xs font-medium text-muted-foreground">
          Path routes <span className="text-2xs text-muted-foreground">— /&lt;prefix&gt;/&lt;port&gt; → host:&lt;port&gt; (Xray-style dynamic forwarding)</span>
        </span>
        <Button variant="ghost" size="sm" onClick={add}><Plus className="h-3.5 w-3.5" /> Add transport</Button>
      </div>
      {routes.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-3 text-center text-2xs text-muted-foreground">
          No path routes. Add <b>ws</b>, <b>httpupgrade</b> or <b>xhttp</b> to route by path with a dynamic port.
        </div>
      ) : (
        <div className="space-y-2">
          {routes.map((r, i) => (
            <div key={i} className="rounded-lg border border-border bg-mutedslate p-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <Select className="w-32" value={r.transport} onChange={(e) => update(i, { transport: e.target.value as PathTransport })}>
                  {(Object.keys(TRANSPORT_META) as PathTransport[]).map((t) => (
                    <option key={t} value={t}>{TRANSPORT_META[t].label}</option>
                  ))}
                </Select>
                <div className="flex items-center gap-1 text-2xs font-mono text-muted-foreground">
                  /<Input className="h-8 w-24 font-mono" placeholder="prefix" value={r.prefix} onChange={(e) => update(i, { prefix: e.target.value })} />/
                  <span className="rounded bg-primary/15 px-1.5 py-0.5 font-bold text-primary">port</span>
                </div>
                <div className="ml-auto flex items-center gap-1">
                  <Input className="h-8 w-20" type="number" placeholder="min port" value={r.min_port || ''} onChange={(e) => update(i, { min_port: parseInt(e.target.value, 10) || 0 })} />
                  <span className="text-2xs text-muted-foreground">—</span>
                  <Input className="h-8 w-20" type="number" placeholder="max port" value={r.max_port || ''} onChange={(e) => update(i, { max_port: parseInt(e.target.value, 10) || 0 })} />
                  <Button variant="ghost" size="sm" className="text-red-500" onClick={() => remove(i)}><Trash2 className="h-3.5 w-3.5" /></Button>
                </div>
              </div>
              <p className="mt-1.5 text-2xs text-muted-foreground">{TRANSPORT_META[r.transport].hint}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------- Preview panel (shows what the config will look like) ----------

export function RoutePreview({ draft }: { draft: Partial<Mapping> }) {
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
    <div className="rounded-lg border border-primary/30 bg-primary/5 p-3 short:p-2 short:rounded-md">
      <div className="mb-1.5 text-2xs font-semibold uppercase tracking-wide text-primary short:mb-0.5">Live preview</div>
      <div className="space-y-1 font-mono text-2xs text-muted-foreground short:space-y-0.5">
        {lines.map((l, i) => <div key={i}>{l}</div>)}
      </div>
    </div>
  )
}

// ---------- Templates gallery ----------

export function TemplateGallery({ templates, onPick }: { templates: MappingTemplate[]; onPick: (t: MappingTemplate) => void }) {
  return (
    <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
      {templates.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => onPick(t)}
          className="group rounded-xl border border-border bg-card p-3.5 text-left transition-all hover:border-primary/40 hover:shadow-md
"
        >
          <div className="mb-1 text-xs font-semibold text-foreground group-hover:text-primary">{t.name}</div>
          <div className="text-2xs leading-relaxed text-muted-foreground">{t.description}</div>
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

