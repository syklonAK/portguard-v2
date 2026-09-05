import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Plus, Pencil, Trash2, RefreshCw, Server, Globe2, KeyRound, Zap, Activity,
  RadioTower, Landmark, Cpu, HardDrive, HeartPulse, Waypoints, Wrench, Download,
} from 'lucide-react'
import { api, type ServerNode, type NodeSummary, type ToolState, type ToolInstallResult } from '../api'
import { Badge, Button, Card, CardHeader, CodeBlock, Empty, Field, Input, Modal, Select, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'

const ROLES: { value: ServerNode['role']; label: string; desc: string }[] = [
  { value: 'generic', label: 'Generic server', desc: 'Any managed PortGuard server (web/app/LB)' },
  { value: 'iran', label: 'Iran hub', desc: 'Tunnel ingress side — users connect here' },
  { value: 'foreign', label: 'Foreign egress', desc: 'Tunnel egress side — traffic exits here' },
]

const ROLE_COLORS: Record<string, 'slate' | 'cyan' | 'blue' | 'purple' | 'green'> = {
  generic: 'slate',
  standalone: 'slate',
  master: 'purple',
  iran: 'cyan',
  foreign: 'blue',
}

function emptyNode(): Partial<ServerNode> {
  return { name: '', host: '', port: 8080, role: 'generic', enabled: true, notes: '' }
}

function fmtAgo(iso: string | null): string {
  if (!iso) return '—'
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

export default function Servers() {
  const qc = useQueryClient()
  const { push } = useToast()

  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.listNodes() })
  const selfInfo = useQuery({ queryKey: ['node-self'], queryFn: () => api.nodeSelf() })

  const [modal, setModal] = useState<null | 'create' | 'edit'>(null)
  const [draft, setDraft] = useState<Partial<ServerNode>>(emptyNode())
  const [tokenDraft, setTokenDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [summaryModal, setSummaryModal] = useState<{ node: ServerNode; data: NodeSummary } | null>(null)
  const [toolsModal, setToolsModal] = useState<ServerNode | null>(null)
  const [remoteTools, setRemoteTools] = useState<ToolState[] | null>(null)
  const [toolsLoading, setToolsLoading] = useState(false)
  const [installingTool, setInstallingTool] = useState<string | null>(null)
  const [toolOutput, setToolOutput] = useState<ToolInstallResult | null>(null)

  const refresh = () => qc.invalidateQueries({ queryKey: ['nodes'] })

  const save = async () => {
    setSaving(true)
    try {
      const payload: Partial<ServerNode> & { api_token?: string } = { ...draft }
      if (tokenDraft.trim()) payload.api_token = tokenDraft.trim()
      if (modal === 'create') {
        await api.createNode(payload)
        push('success', 'Server profile created.')
      } else if (modal === 'edit' && draft.id) {
        await api.updateNode(draft.id, payload)
        push('success', 'Server profile updated.')
      }
      setModal(null)
      setTokenDraft('')
      refresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setSaving(false)
    }
  }

  const del = useMutation({
    mutationFn: (id: number) => api.deleteNode(id),
    onSuccess: () => {
      push('success', 'Server removed from management.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const probe = useMutation({
    mutationFn: async (n: ServerNode) => {
      await api.nodeAction(n.id, 'probe')
      return api.nodeAction(n.id, 'summary').then(() => undefined)
    },
    onSuccess: () => {
      push('success', 'Server is online.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const remoteApply = useMutation({
    mutationFn: (n: ServerNode) => api.nodeAction(n.id, 'apply'),
    onSuccess: () => push('success', 'Apply triggered on the remote server.'),
    onError: (e: any) => push('error', e.message),
  })

  const openSummary = async (n: ServerNode) => {
    try {
      const data = await api.nodeAction(n.id, 'summary')
      setSummaryModal({ node: n, data: data as NodeSummary })
      refresh()
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const openTools = async (n: ServerNode) => {
    setToolsModal(n)
    setRemoteTools(null)
    setToolOutput(null)
    setToolsLoading(true)
    try {
      const res = await api.nodeTools(n.id)
      setRemoteTools(res.tools)
    } catch (e: any) {
      push('error', e.message)
      setToolsModal(null)
    } finally {
      setToolsLoading(false)
    }
  }

  const installRemote = async (toolId: string) => {
    if (!toolsModal) return
    setInstallingTool(toolId)
    setToolOutput(null)
    try {
      const res = await api.nodeInstallTool(toolsModal.id, toolId)
      setToolOutput(res)
      if (res.ok) {
        push('success', `${toolId} installed on ${toolsModal.name} (${res.elapsed}).`)
        const refreshed = await api.nodeTools(toolsModal.id)
        setRemoteTools(refreshed.tools)
      } else {
        push('error', `${toolId} install reported problems on ${toolsModal.name}.`)
      }
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setInstallingTool(null)
    }
  }

  const selfRole = selfInfo.data?.role || 'standalone'

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Servers</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Manage every PortGuard server from this panel — profiles, tunnel roles and remote operations.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => { nodes.refetch(); selfInfo.refetch() }}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
          <Button onClick={() => { setDraft(emptyNode()); setTokenDraft(''); setModal('create') }}>
            <Plus className="h-4 w-4" /> Add server
          </Button>
        </div>
      </div>

      {/* This panel's own node identity */}
      <Card>
        <CardHeader
          title="This panel"
          desc="How other masters see this server (node API handshake)"
          right={<Badge color={ROLE_COLORS[selfRole] || 'slate'}>{selfRole}</Badge>}
        />
        <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2">
          <Field label="Node role" hint="standalone = not managed by anyone; iran/foreign = tunnel side">
            <Select
              value={selfRole}
              onChange={async (e) => {
                try {
                  await api.putNodeSelf({ role: e.target.value })
                  push('success', 'Role saved.')
                  selfInfo.refetch()
                } catch (err: any) {
                  push('error', err.message)
                }
              }}
            >
              <option value="standalone">standalone</option>
              <option value="iran">iran (tunnel hub)</option>
              <option value="foreign">foreign (tunnel egress)</option>
              <option value="master">master</option>
              <option value="generic">generic</option>
            </Select>
          </Field>
          <Field
            label="Node API token"
            hint={selfInfo.data?.configured ? 'configured — masters can authenticate' : 'not configured — this server is not manageable remotely'}
          >
            <div className="flex gap-2">
              <Input
                type="password"
                placeholder={selfInfo.data?.configured ? '•••••••• (set a new one to rotate)' : 'generate & paste a strong token'}
                value={tokenDraft}
                onChange={(e) => setTokenDraft(e.target.value)}
              />
              <Button
                variant="secondary"
                onClick={async () => {
                  const t = crypto.randomUUID().replace(/-/g, '')
                  setTokenDraft(t)
                }}
                title="Generate"
              >
                <KeyRound className="h-4 w-4" />
              </Button>
              <Button
                disabled={!tokenDraft.trim()}
                onClick={async () => {
                  try {
                    await api.putNodeSelf({ token: tokenDraft.trim() })
                    push('success', 'Node token saved — paste it into the master\'s server profile.')
                    setTokenDraft('')
                    selfInfo.refetch()
                  } catch (err: any) {
                    push('error', err.message)
                  }
                }}
              >
                Save
              </Button>
            </div>
          </Field>
        </div>
      </Card>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {nodes.isLoading ? (
          <div className="flex justify-center py-14 sm:col-span-2 xl:col-span-3"><Spinner /></div>
        ) : !nodes.data?.length ? (
          <div className="sm:col-span-2 xl:col-span-3">
            <Empty message="No servers yet. Add a profile, paste its node token and manage it from here." />
          </div>
        ) : (
          nodes.data.map((n) => (
            <Card key={n.id} className={`flex flex-col ${!n.enabled ? 'opacity-60' : ''}`}>
              <div className="flex items-start justify-between p-5 pb-3">
                <div className="flex items-center gap-3">
                  <div className={`flex h-10 w-10 items-center justify-center rounded-xl ${
                    n.role === 'iran' ? 'bg-cyan-100 text-cyan-600 dark:bg-cyan-500/15 dark:text-cyan-300'
                    : n.role === 'foreign' ? 'bg-blue-100 text-blue-600 dark:bg-blue-500/15 dark:text-blue-300'
                    : 'bg-slate-100 text-slate-600 dark:bg-slate-500/15 dark:text-slate-300'}`}>
                    {n.role === 'iran' ? <RadioTower className="h-5 w-5" /> :
                     n.role === 'foreign' ? <Globe2 className="h-5 w-5" /> :
                     <Server className="h-5 w-5" />}
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-semibold">{n.name}</span>
                      <Badge color={ROLE_COLORS[n.role] || 'slate'}>{n.role}</Badge>
                    </div>
                    <div className="font-mono text-2xs text-slate-400">{n.host}:{n.port}</div>
                  </div>
                </div>
                <span className={`flex items-center gap-1.5 rounded-md px-1.5 py-0.5 text-2xs font-medium ${
                  n.status === 'online' ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                  : n.status === 'offline' ? 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
                  : 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400'}`}>
                  <span className={`h-1.5 w-1.5 rounded-full ${n.status === 'online' ? 'bg-emerald-500' : n.status === 'offline' ? 'bg-red-500' : 'bg-slate-400'}`} />
                  {n.status}
                </span>
              </div>

              <div className="px-5 pb-3 text-2xs text-slate-400">
                {n.notes || '—'} · seen {fmtAgo(n.last_seen)}
              </div>

              <div className="mt-auto flex flex-wrap items-center gap-1.5 border-t border-slate-200 p-4 dark:border-slate-800">
                <Button variant="secondary" size="sm" onClick={() => probe.mutate(n)} disabled={probe.isPending}>
                  <Activity className="h-3.5 w-3.5" /> Probe
                </Button>
                <Button variant="secondary" size="sm" onClick={() => openSummary(n)}>
                  <Cpu className="h-3.5 w-3.5" /> Overview
                </Button>
                <Button variant="secondary" size="sm" onClick={() => openTools(n)}>
                  <Wrench className="h-3.5 w-3.5" /> Tools
                </Button>
                <Button variant="secondary" size="sm" onClick={() => remoteApply.mutate(n)} disabled={remoteApply.isPending}>
                  <Zap className="h-3.5 w-3.5" /> Apply
                </Button>
                <span className="flex-1" />
                <Toggle
                  checked={n.enabled}
                  onChange={async () => {
                    try {
                      await api.updateNode(n.id, { ...n, enabled: !n.enabled })
                      refresh()
                    } catch (e: any) {
                      push('error', e.message)
                    }
                  }}
                />
                <Button variant="ghost" size="sm" onClick={() => { setDraft(JSON.parse(JSON.stringify(n))); setTokenDraft(''); setModal('edit') }}>
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
                <Button variant="ghost" size="sm" className="text-red-500" onClick={() => del.mutate(n.id)}>
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </Card>
          ))
        )}
      </div>

      <Card>
        <CardHeader title="How it works" desc="Master ↔ node handshake" />
        <div className="space-y-2.5 p-5 text-xs leading-relaxed text-slate-600 dark:text-slate-400">
          <p><b>1.</b> Install PortGuard on every server (same one-liner). Each panel is fully standalone.</p>
          <p><b>2.</b> On the servers you want to manage remotely: set a <b>Node API token</b> above (or on that server's Settings → panel) — it becomes a manageable node.</p>
          <p><b>3.</b> On this master: <b>Add server</b>, fill host/port, paste that token, choose the role (generic / iran hub / foreign egress).</p>
          <p><b>4.</b> Probe checks reachability, Overview pulls live CPU/RAM/mappings/health/tunnel state, Apply runs the safe render→validate→backup→reload pipeline on the remote server.</p>
          <p className="rounded-lg bg-sky-50 p-2.5 text-2xs text-sky-700 dark:bg-sky-500/10 dark:text-sky-300">
            Tunnel tip: mark the <b>Iran hub</b> and <b>Foreign egress</b> roles on the two servers, then use Tunnels on the Iran side to configure the relays.
          </p>
        </div>
      </Card>

      {/* create/edit modal */}
      <Modal open={modal !== null} onClose={() => setModal(null)} title={modal === 'create' ? 'Add server profile' : `Edit — ${draft.name}`}>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Name"><Input value={draft.name || ''} onChange={(e) => setDraft({ ...draft, name: e.target.value })} placeholder="DE-egress-01" /></Field>
            <Field label="Role">
              <Select value={draft.role || 'generic'} onChange={(e) => setDraft({ ...draft, role: e.target.value as ServerNode['role'] })}>
                {ROLES.map((r) => <option key={r.value} value={r.value}>{r.label} — {r.desc}</option>)}
              </Select>
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Host" hint="IP or domain of the node panel">
              <Input value={draft.host || ''} onChange={(e) => setDraft({ ...draft, host: e.target.value })} placeholder="10.0.0.5" />
            </Field>
            <Field label="Panel port"><Input type="number" value={draft.port || ''} onChange={(e) => setDraft({ ...draft, port: parseInt(e.target.value, 10) })} /></Field>
          </div>
          <Field
            label="Node API token"
            hint={modal === 'edit' ? 'leave blank to keep the current token' : 'from the server\'s "This panel" node-token field'}
          >
            <Input type="password" value={tokenDraft} onChange={(e) => setTokenDraft(e.target.value)} placeholder="paste the node token" />
          </Field>
          <Field label="Notes"><Input value={draft.notes || ''} onChange={(e) => setDraft({ ...draft, notes: e.target.value })} /></Field>
          <div className="flex justify-end gap-2 border-t border-slate-200 pt-4 dark:border-slate-700">
            <Button variant="secondary" onClick={() => setModal(null)}>Cancel</Button>
            <Button onClick={save} disabled={saving || !draft.name || !draft.host}>{saving ? 'Saving…' : 'Save server'}</Button>
          </div>
        </div>
      </Modal>

      {/* remote tools modal */}
      <Modal open={toolsModal !== null} onClose={() => setToolsModal(null)} wide
        title={toolsModal ? `Tools on ${toolsModal.name} (${toolsModal.host})` : ''}>
        {toolsModal && (
          <div className="space-y-4">
            {toolsLoading ? (
              <div className="flex justify-center py-12"><Spinner /></div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                      <th className="px-2 py-2.5 font-medium">Tool</th>
                      <th className="px-2 py-2.5 font-medium">State</th>
                      <th className="px-2 py-2.5 font-medium">Binary</th>
                      <th className="px-2 py-2.5 text-right font-medium">Action</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
                    {(remoteTools ?? []).map((t) => (
                      <tr key={t.id}>
                        <td className="px-2 py-2.5 font-medium">{t.name}</td>
                        <td className="px-2 py-2.5">
                          {t.installed ? (
                            <Badge color="green">{t.version || 'installed'}</Badge>
                          ) : (
                            <Badge color="red">missing</Badge>
                          )}
                        </td>
                        <td className="px-2 py-2.5 font-mono text-2xs text-slate-400">{t.binary || '—'}</td>
                        <td className="px-2 py-2.5">
                          <Button
                            size="sm"
                            variant={t.installed ? 'secondary' : 'primary'}
                            disabled={installingTool === t.id}
                            onClick={() => installRemote(t.id)}
                          >
                            <Download className={`h-3.5 w-3.5 ${installingTool === t.id ? 'animate-bounce' : ''}`} />
                            {installingTool === t.id ? 'Installing…' : t.installed ? 'Reinstall' : 'Install'}
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {toolOutput && (
              <div>
                <div className="mb-1.5 flex items-center gap-2">
                  <Badge color={toolOutput.ok ? 'green' : 'red'}>{toolOutput.ok ? 'ok' : 'error'}</Badge>
                  <span className="text-2xs text-slate-400">finished in {toolOutput.elapsed}</span>
                </div>
                <CodeBlock code={toolOutput.output || '(no output)'} />
              </div>
            )}
            <p className="text-2xs leading-relaxed text-slate-400">
              The installer runs on the remote server through its node API (official installers only).
              After installing <b>xray</b> for the tunnel bridge, disable the default xray service there
              (<code>systemctl disable --now xray</code>) — PortGuard runs the bridge as its own unit.
            </p>
          </div>
        )}
      </Modal>

      {/* remote overview modal */}
      <Modal open={summaryModal !== null} onClose={() => setSummaryModal(null)} wide
        title={summaryModal ? `${summaryModal.node.name} — live overview` : ''}>
        {summaryModal && (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <Stat label="CPU" value={`${summaryModal.data.system.cpu_percent.toFixed(1)}%`} icon={<Cpu className="h-4 w-4" />} />
              <Stat label="RAM" value={`${summaryModal.data.system.mem_percent.toFixed(1)}%`} icon={<Landmark className="h-4 w-4" />} />
              <Stat label="Disk" value={`${summaryModal.data.system.disk_percent.toFixed(1)}%`} icon={<HardDrive className="h-4 w-4" />} />
              <Stat label="Uptime" value={fmtUptimeShort(summaryModal.data.system.uptime)} icon={<HeartPulse className="h-4 w-4" />} />
            </div>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <Stat label="Mappings" value={`${summaryModal.data.mappings.enabled}/${summaryModal.data.mappings.total}`} />
              <Stat label="Unmanaged ports" value={String(summaryModal.data.ports.unmanaged)} />
              <Stat label="Backends up/down" value={`${summaryModal.data.health.up}/${summaryModal.data.health.down}`} />
              <Stat label="PortGuard" value={summaryModal.data.version} />
            </div>
            <div className="rounded-xl border border-slate-200 p-4 dark:border-slate-700">
              <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-slate-600 dark:text-slate-300">
                <Waypoints className="h-4 w-4" /> Tunnel state
              </div>
              <div className="grid grid-cols-2 gap-2 text-2xs sm:grid-cols-4">
                <span>hedioum: <b>{summaryModal.data.tunnel.hedioum_installed ? summaryModal.data.tunnel.hedioum_active : 'absent'}</b></span>
                <span>xray: <b>{summaryModal.data.tunnel.xray_installed ? 'installed' : 'missing'}</b></span>
                <span>bridge: <b>{summaryModal.data.tunnel.bridge_active}</b></span>
                <span>socks: <b>{summaryModal.data.tunnel.socks_listening || '—'}</b></span>
              </div>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}

function Stat({ label, value, icon }: { label: string; value: string; icon?: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-slate-200 p-3 dark:border-slate-700">
      <div className="flex items-center gap-1.5 text-2xs font-medium uppercase tracking-wide text-slate-400">
        {icon} {label}
      </div>
      <div className="mt-1 text-sm font-bold">{value}</div>
    </div>
  )
}

function fmtUptimeShort(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  if (d > 0) return `${d}d ${h}h`
  const m = Math.floor((sec % 3600) / 60)
  return `${h}h ${m}m`
}
