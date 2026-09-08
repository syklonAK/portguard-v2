import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, RefreshCw, ShieldCheck, ArrowRight, CheckCircle2, XCircle, Zap, RadioTower, Globe2, Activity, Download } from 'lucide-react'
import { api, type TunnelRelay, type TunnelStatus, type PingTunnelStatus } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Field, Input, Modal, Select, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'
import TrojanSuite from '../components/tunnels/TrojanSuite'

function emptyRelay(): Partial<TunnelRelay> {
  return {
    name: '',
    mode: 'tls',
    enabled: true,
    target_host: '',
    target_port: 443,
    listen_ip: '0.0.0.0',
    listen_port: 443,
    bridge_port: 21000,
    udp: false,
    host_header: '',
    domain: '',
    ssl_cert_id: null,
    notes: '',
  }
}

function StateBadge({ state }: { state?: string }) {
  if (!state) return <Badge color="slate">unknown</Badge>
  if (state === 'active' || state === 'iran' || state === 'installed')
    return <Badge color="green">{state}</Badge>
  if (state === 'foreign') return <Badge color="blue">{state}</Badge>
  return <Badge color="red">{state}</Badge>
}

function EnvRow({ label, ok, detail, missing }: { label: string; ok: boolean; detail?: string; missing?: string }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {ok ? (
        <span className="flex items-center gap-1.5 text-2xs text-emerald-600 dark:text-emerald-400">
          <CheckCircle2 className="h-3.5 w-3.5" /> {detail || 'ok'}
        </span>
      ) : (
        <span className="flex items-center gap-1.5 text-2xs text-red-500">
          <XCircle className="h-3.5 w-3.5" /> {missing || 'not installed'}
        </span>
      )}
    </div>
  )
}

export default function Tunnels() {
  const qc = useQueryClient()
  const { push } = useToast()

  const status = useQuery({ queryKey: ['tunnel-status'], queryFn: () => api.tunnelStatus() })
  const ptStatus = useQuery({ queryKey: ['pingtunnel-status'], queryFn: () => api.pingTunnelStatus() })
  const relays = useQuery({ queryKey: ['relays'], queryFn: () => api.listRelays() })
  const certs = useQuery({ queryKey: ['certs'], queryFn: () => api.listCerts() })
  const selfInfo = useQuery({ queryKey: ['node-self'], queryFn: () => api.nodeSelf() })

  const [modal, setModal] = useState<null | 'create' | 'edit'>(null)
  const [draft, setDraft] = useState<Partial<TunnelRelay>>(emptyRelay())
  const [saving, setSaving] = useState(false)
  // ICMP tunnel (pingtunnel)
  const [ptSide, setPtSide] = useState<null | 'iran' | 'foreign'>(null)
  const [ptDraft, setPtDraft] = useState({ port: 8443, foreign_ip: '', target_port: 8443 })
  const [ptBusy, setPtBusy] = useState(false)

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['tunnel-status'] })
    qc.invalidateQueries({ queryKey: ['relays'] })
  }

  const set = <K extends keyof TunnelRelay>(k: K, v: TunnelRelay[K]) => setDraft((d) => ({ ...d, [k]: v }))

  const save = async () => {
    setSaving(true)
    try {
      if (modal === 'create') {
        await api.createRelay(draft)
        push('success', 'Relay created. Apply to activate it.')
      } else if (modal === 'edit' && draft.id) {
        await api.updateRelay(draft.id, draft)
        push('success', 'Relay updated. Apply to activate it.')
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
    mutationFn: (id: number) => api.deleteRelay(id),
    onSuccess: () => {
      push('success', 'Relay deleted. Apply to update the bridge.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const validate = useMutation({
    mutationFn: () => api.tunnelValidate(),
    onSuccess: (res) => {
      if (res.ok) push('success', res.note || 'Bridge config is valid.')
      else push('error', res.error || 'Validation failed')
    },
    onError: (e: any) => push('error', e.message),
  })

  const apply = useMutation({
    mutationFn: () => api.tunnelApply(),
    onSuccess: (res) => {
      push('success', `Bridge applied (${res.relays} relay(s)).`)
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const ptRefresh = () => qc.invalidateQueries({ queryKey: ['pingtunnel-status'] })

  const ptInstall = useMutation({
    mutationFn: () => api.pingTunnelInstall(),
    onSuccess: () => {
      push('success', 'PingTunnel core installed.')
      ptRefresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const ptCreate = async () => {
    if (!ptSide) return
    setPtBusy(true)
    try {
      const body = ptSide === 'iran'
        ? { side: 'iran' as const, port: ptDraft.port, foreign_ip: ptDraft.foreign_ip, target_port: ptDraft.target_port }
        : { side: 'foreign' as const }
      const res = await api.pingTunnelCreate(body)
      push('success', `ICMP tunnel created (${res.unit}).`)
      setPtSide(null)
      ptRefresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setPtBusy(false)
    }
  }

  const ptDelete = async (unit: string) => {
    try {
      await api.pingTunnelDelete(unit)
      push('success', `${unit} removed.`)
      ptRefresh()
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const st: TunnelStatus | undefined = status.data
  const pt: PingTunnelStatus | undefined = ptStatus.data
  const myRole = selfInfo.data?.role || 'standalone'

  const setRole = useMutation({
    mutationFn: (role: string) => api.putNodeSelf({ role }),
    onSuccess: () => {
      push('success', 'This server\'s tunnel role saved.')
      qc.invalidateQueries({ queryKey: ['node-self'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Tunnels — Hedioum</h1>
          <p className="text-xs text-muted-foreground">
            Iran-side relays: users → this server → SOCKS5 hub → foreign egress → node.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => { status.refetch(); relays.refetch() }}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
          <Button variant="secondary" onClick={() => validate.mutate()} disabled={validate.isPending || !st?.xray_installed}>
            <ShieldCheck className="h-4 w-4" /> Validate
          </Button>
          <Button variant="success" onClick={() => apply.mutate()} disabled={apply.isPending || !st?.xray_installed}>
            <Zap className="h-4 w-4" /> Apply bridge
          </Button>
          <Button onClick={() => { setDraft(emptyRelay()); setModal('create') }} disabled={!st?.xray_installed}>
            <Plus className="h-4 w-4" /> New relay
          </Button>
        </div>
      </div>

      {st && !st.xray_installed && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
          <b>xray-core is not installed on this server.</b> The bridge needs the xray binary:
          <code className="mx-1 rounded bg-amber-100 px-1.5 py-0.5 dark:bg-amber-500/20">
            bash -c "$(curl -fsSL https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install
          </code>
          then disable the default xray service (<code className="rounded bg-amber-100 px-1 dark:bg-amber-500/20">systemctl disable --now xray</code>) — PortGuard runs it as its own unit.
        </div>
      )}

      <Card>
        <CardHeader
          title="This server's tunnel role"
          desc="Declare which side of the 2-server topology this box plays (also shown on the master's Servers page)"
        />
        <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-3">
          <RoleCard
            active={myRole === 'iran'}
            icon={<RadioTower className="h-5 w-5" />}
            title="Iran hub (ingress)"
            desc="Users connect here. Configure relays below; traffic rides the tunnel to the foreign egress."
            onClick={() => setRole.mutate('iran')}
          />
          <RoleCard
            active={myRole === 'foreign'}
            icon={<Globe2 className="h-5 w-5" />}
            title="Foreign egress"
            desc="Traffic exits here. Run the Hedioum egress setup on this box; its token feeds the Iran side."
            onClick={() => setRole.mutate('foreign')}
          />
          <RoleCard
            active={myRole !== 'iran' && myRole !== 'foreign'}
            icon={<CheckCircle2 className="h-5 w-5" />}
            title="Not a tunnel server"
            desc="This panel is standalone or a plain managed server."
            onClick={() => setRole.mutate('standalone')}
          />
        </div>
        {myRole === 'foreign' && (
          <div className="border-t border-border px-5 py-4 text-2xs leading-relaxed text-amber-600 dark:text-amber-400">
            This box is the egress: relays configured below are meaningless here — set up the egress with the official
            hedioum-tunnel script and give the printed token to the Iran hub. Relay management belongs on the Iran server.
          </div>
        )}
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader title="Relays" desc="Each relay forwards one public entry to a foreign node through the tunnel (Iran side)" />
          {relays.isLoading ? (
            <div className="flex justify-center py-12"><Spinner /></div>
          ) : !relays.data?.length ? (
            <Empty message="No relays yet. Add one to start forwarding through the tunnel." />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                    <th className="px-5 py-2.5 font-medium">Name</th>
                    <th className="px-3 py-2.5 font-medium">Mode</th>
                    <th className="px-3 py-2.5 font-medium">Entry point</th>
                    <th className="px-3 py-2.5 font-medium">Foreign node</th>
                    <th className="px-3 py-2.5 font-medium">Enabled</th>
                    <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {relays.data.map((r) => (
                    <tr key={r.id} className={`hover:bg-muted/60 ${!r.enabled ? 'opacity-60' : ''}`}>
                      <td className="px-5 py-3 font-medium">{r.name}</td>
                      <td className="px-3 py-3"><Badge color={r.mode === 'tls' ? 'purple' : 'blue'}>{r.mode}</Badge></td>
                      <td className="px-3 py-3 font-mono text-xs">
                        {r.mode === 'raw' ? `${r.listen_ip}:${r.listen_port}` : (
                          <span title="nginx vhost (Mappings) + local bridge port">{r.domain} → 127.0.0.1:{r.bridge_port}</span>
                        )}
                      </td>
                      <td className="px-3 py-3 font-mono text-xs">
                        {r.target_host}:{r.target_port}
                        {r.host_header && <span className="block text-2xs text-muted-foreground">Host: {r.host_header}</span>}
                      </td>
                      <td className="px-3 py-3">
                        <Toggle checked={r.enabled} onChange={() => api.updateRelay(r.id, { ...r, enabled: !r.enabled }).then(refresh).catch((e: any) => push('error', e.message))} />
                      </td>
                      <td className="px-5 py-3">
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => { setDraft(JSON.parse(JSON.stringify(r))); setModal('edit') }}><Pencil className="h-3.5 w-3.5" /></Button>
                          <Button variant="ghost" size="sm" className="text-red-500" onClick={() => del.mutate(r.id)}><Trash2 className="h-3.5 w-3.5" /></Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card>
          <CardHeader title="Environment" desc="Detected on this server" />
          {status.isLoading ? (
            <div className="flex justify-center py-12"><Spinner /></div>
          ) : st ? (
            <div className="space-y-2 p-4">
              <EnvRow label="hedioum-tunnel" ok={st.hedioum_installed} detail={st.hedioum_version || st.hedioum_binary} missing="not installed" />
              <EnvRow label="hedioum service" ok={st.hedioum_active === 'active'} detail={st.hedioum_active} missing={st.hedioum_active} />
              <EnvRow label="xray-core" ok={st.xray_installed} detail={st.xray_version} missing="not installed (see hint above)" />
              <EnvRow label="bridge service" ok={st.bridge_active === 'active'} detail={st.bridge_active} missing="not applied yet" />
              <EnvRow label="SOCKS5 hub" ok={!!st.socks_listening} detail={st.socks_listening} missing="no 40000-49999 loopback listener" />
              <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2">
                <span className="text-xs font-medium text-muted-foreground">Detected role</span>
                <StateBadge state={st.role} />
              </div>
              {st.role === 'iran' && st.socks_listening && (
                <p className="rounded-lg bg-sky-50 p-2.5 text-2xs leading-relaxed text-sky-700 dark:bg-sky-500/10 dark:text-sky-300">
                  Iran hub detected. Set the SOCKS host/port in Settings → Tunnel to match ({st.socks_listening}) so the bridge routes through it.
                </p>
              )}
            </div>
          ) : null}
        </Card>
      </div>

      <TrojanSuite status={st} />

      <Card>
        <CardHeader
          title="ICMP tunnel (PingTunnel)"
          desc="Tunnel TCP over ICMP echo — works where UDP/TCP to the foreign side is throttled. Iran side forwards a local port; foreign side runs the ICMP server."
          right={<Badge color={pt?.installed ? 'green' : 'red'}>{pt?.installed ? (pt.version || 'installed') : 'not installed'}</Badge>}
        />
        {pt && !pt.installed && (
          <div className="border-b border-border px-5 py-4">
            <Button variant="secondary" size="sm" onClick={() => ptInstall.mutate()} disabled={ptInstall.isPending}>
              <Download className="h-3.5 w-3.5" /> Install PingTunnel core (v2.8)
            </Button>
            <span className="ml-3 text-2xs text-muted-foreground">downloads the pinned upstream release for this architecture</span>
          </div>
        )}
        <div className="space-y-3 p-5">
          {pt && !pt.icmp_echo_ignored && pt.installed && (
            <div className="rounded-lg border border-amber-200 bg-amber-50 p-2.5 text-2xs text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
              The kernel still answers ICMP echo itself — pingtunnel needs it silent. Applying any tunnel (or the core install) sets
              <code className="mx-1 rounded bg-amber-100 px-1 dark:bg-amber-500/20">net.ipv4.icmp_echo_ignore_all=1</code> automatically.
            </div>
          )}

          {pt && pt.services.length > 0 && (
            <div className="overflow-x-auto rounded-xl border border-border">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                    <th className="px-4 py-2 font-medium">Service</th>
                    <th className="px-3 py-2 font-medium">Role</th>
                    <th className="px-3 py-2 font-medium">Port / Target</th>
                    <th className="px-3 py-2 font-medium">State</th>
                    <th className="px-4 py-2 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {pt.services.map((u) => (
                    <tr key={u.unit}>
                      <td className="px-4 py-2.5 font-mono text-xs">{u.unit}</td>
                      <td className="px-3 py-2.5"><Badge color={u.role === 'iran' ? 'cyan' : 'blue'}>{u.role}</Badge></td>
                      <td className="px-3 py-2.5 font-mono text-xs">
                        {u.role === 'iran' ? `:${u.port} → ${u.target || '?'}` : 'ICMP server'}
                      </td>
                      <td className="px-3 py-2.5"><StateBadge state={u.active} /></td>
                      <td className="px-4 py-2.5 text-right">
                        <Button variant="ghost" size="sm" className="text-red-500" onClick={() => ptDelete(u.unit)}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {pt && pt.services.length === 0 && (
            <Empty message="No ICMP tunnels yet. Configure one for the Iran or the foreign server." />
          )}

          {!ptSide ? (
            <div className="flex gap-2">
              <Button size="sm" disabled={!pt?.installed} onClick={() => { setPtSide('iran'); setPtDraft({ port: 8443, foreign_ip: '', target_port: 8443 }) }}>
                <Plus className="h-3.5 w-3.5" /> Iran side (client)
              </Button>
              <Button size="sm" variant="secondary" disabled={!pt?.installed} onClick={() => setPtSide('foreign')}>
                <Plus className="h-3.5 w-3.5" /> Foreign side (server)
              </Button>
            </div>
          ) : (
            <div className="space-y-3 rounded-xl border border-border p-4">
              <div className="text-xs font-bold">{ptSide === 'iran' ? 'New ICMP tunnel — Iran (client)' : 'New ICMP tunnel — Foreign (server)'}</div>
              {ptSide === 'iran' ? (
                <>
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                    <Field label="Foreign server IP">
                      <Input value={ptDraft.foreign_ip} onChange={(e) => setPtDraft({ ...ptDraft, foreign_ip: e.target.value })} placeholder="5.6.7.8" />
                    </Field>
                    <Field label="Tunnel port (local)" hint="the xray inbound this tunnel serves">
                      <Input type="number" value={ptDraft.port} onChange={(e) => setPtDraft({ ...ptDraft, port: parseInt(e.target.value, 10) })} />
                    </Field>
                    <Field label="Target port (on foreign)" hint="the real service port on the foreign box">
                      <Input type="number" value={ptDraft.target_port} onChange={(e) => setPtDraft({ ...ptDraft, target_port: parseInt(e.target.value, 10) })} />
                    </Field>
                  </div>
                  <p className="text-2xs leading-relaxed text-muted-foreground">
                    Traffic flow: user → <code>this box :port</code> → ICMP → foreign → <code>127.0.0.1:target_port</code> (its xray inbound).
                    The foreign server must have run the <b>foreign side</b> once, and this port must be free here.
                  </p>
                </>
              ) : (
                <p className="text-2xs leading-relaxed text-muted-foreground">
                  Registers the ICMP echo server unit (<code>pingtunnel-kharej.service</code>). ICMP itself must be allowed through this
                  server's firewall — most providers do by default.
                </p>
              )}
              <div className="flex justify-end gap-2">
                <Button variant="secondary" size="sm" onClick={() => setPtSide(null)}>Cancel</Button>
                <Button size="sm" onClick={ptCreate}
                  disabled={ptBusy || (ptSide === 'iran' && (!ptDraft.foreign_ip || !ptDraft.port || !ptDraft.target_port))}>
                  {ptBusy ? 'Creating…' : 'Create tunnel'}
                </Button>
              </div>
            </div>
          )}
        </div>
      </Card>

      <Card>
        <CardHeader
          title="How the topology works"
          desc="PortGuard automates the Iran side of the hedioum-suite relay bridge"
        />
        <div className="space-y-2 p-5 text-xs leading-relaxed text-muted-foreground">
          <div className="flex flex-wrap items-center gap-2 font-mono">
            <span className="rounded-lg bg-sky-100 px-2 py-1 dark:bg-sky-500/20">user</span>
            <ArrowRight className="h-3 w-3 text-muted-foreground" />
            <span className="rounded-lg bg-sky-100 px-2 py-1 dark:bg-sky-500/20">Iran: entry</span>
            <ArrowRight className="h-3 w-3 text-muted-foreground" />
            <span className="rounded-lg bg-violet-100 px-2 py-1 dark:bg-violet-500/20">xray dokodemo (bridge)</span>
            <ArrowRight className="h-3 w-3 text-muted-foreground" />
            <span className="rounded-lg bg-violet-100 px-2 py-1 dark:bg-violet-500/20">SOCKS5 hub</span>
            <ArrowRight className="h-3 w-3 text-muted-foreground" />
            <span className="rounded-lg bg-emerald-100 px-2 py-1 dark:bg-emerald-500/20">Hedioum egress</span>
          </div>
          <p><b>raw mode</b> — the bridge listens publicly on <code>listen_ip:listen_port</code> and passes TCP straight through; TLS/REALITY stays on the foreign node.</p>
          <p><b>tls mode</b> — terminate TLS locally: create an https mapping (engine: nginx) with the same domain, proxying to <code>127.0.0.1:bridge_port</code> and the foreign node's Host header; the bridge then relays through the SOCKS hub.</p>
          <p>The bridge config (<code>/etc/hedioum-suite/portguard-bridge.json</code>) is generated, validated with <code>xray run -test</code>, backed up, applied atomically and rolled back automatically on failure — same pipeline as nginx/haproxy.</p>
        </div>
      </Card>

      <Modal open={modal !== null} onClose={() => setModal(null)} wide
        title={modal === 'create' ? 'New tunnel relay' : `Edit relay — ${draft.name}`}>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Name" hint="letters, digits, dashes">
              <Input value={draft.name || ''} onChange={(e) => set('name', e.target.value)} placeholder="DE-01" />
            </Field>
            <Field label="Mode">
              <Select value={draft.mode} onChange={(e) => set('mode', e.target.value as 'raw' | 'tls')}>
                <option value="tls">tls — terminate TLS here (nginx vhost)</option>
                <option value="raw">raw — public TCP passthrough</option>
              </Select>
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label="Foreign node address" hint="public IP or domain of the egress/node">
              <Input value={draft.target_host || ''} onChange={(e) => set('target_host', e.target.value)} placeholder="de.example.com" />
            </Field>
            <Field label="Foreign node port">
              <Input type="number" value={draft.target_port || ''} onChange={(e) => set('target_port', parseInt(e.target.value, 10))} />
            </Field>
          </div>

          {draft.mode === 'raw' ? (
            <div className="grid grid-cols-2 gap-3">
              <Field label="Listen IP" hint="0.0.0.0 = all interfaces">
                <Input value={draft.listen_ip || '0.0.0.0'} onChange={(e) => set('listen_ip', e.target.value)} />
              </Field>
              <Field label="Listen port" hint="public port users connect to">
                <Input type="number" value={draft.listen_port || ''} onChange={(e) => set('listen_port', parseInt(e.target.value, 10))} />
              </Field>
            </div>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Public domain" hint="server_name of the local nginx vhost">
                  <Input value={draft.domain || ''} onChange={(e) => set('domain', e.target.value)} placeholder="relay.example.com" />
                </Field>
                <Field label="Bridge port" hint="local dokodemo port (vhost proxies here)">
                  <Input type="number" value={draft.bridge_port || ''} onChange={(e) => set('bridge_port', parseInt(e.target.value, 10))} />
                </Field>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Host header" hint="sent to the foreign node (its server_name)">
                  <Input value={draft.host_header || ''} onChange={(e) => set('host_header', e.target.value)} placeholder="node.example.com" />
                </Field>
                <Field label="TLS certificate">
                  <Select value={draft.ssl_cert_id ?? ''} onChange={(e) => set('ssl_cert_id', e.target.value ? parseInt(e.target.value, 10) : null)}>
                    <option value="">— select certificate —</option>
                    {certs.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.type})</option>)}
                  </Select>
                </Field>
              </div>
            </>
          )}

          <label className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <Toggle checked={!!draft.udp} onChange={(v) => set('udp', v)} /> Forward UDP too (raw mode only meaningful)
          </label>

          <Field label="Notes (optional)">
            <Input value={draft.notes || ''} onChange={(e) => set('notes', e.target.value)} />
          </Field>

          <div className="flex justify-end gap-2 border-t border-border pt-4">
            <Button variant="secondary" onClick={() => setModal(null)}>Cancel</Button>
            <Button onClick={save} disabled={saving || !draft.name || !draft.target_host}>
              {saving ? 'Saving…' : 'Save relay'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

function RoleCard({ active, icon, title, desc, onClick }: {
  active: boolean
  icon: React.ReactNode
  title: string
  desc: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-xl border p-4 text-left transition-all ${
        active
          ? 'border-primary/40 bg-primary/10'
          : 'border-border hover:border-primary/40'
      }`}
    >
      <div className={`mb-2 flex h-9 w-9 items-center justify-center rounded-lg ${
        active ? 'bg-primary/15 text-primary' : 'bg-muted text-muted-foreground'
      }`}>
        {icon}
      </div>
      <div className="text-xs font-bold">{title}</div>
      <p className="mt-1 text-2xs leading-relaxed text-muted-foreground">{desc}</p>
    </button>
  )
}
